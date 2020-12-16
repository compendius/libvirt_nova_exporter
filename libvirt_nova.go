// Written by Charles Short
// 17/11/2020  - first
// 18/11/2020  - updated to use a custom collector interface
// A libvirt prometheus backend providing metrics for Openstack Nova instances
//THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//SOFTWARE.

package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	libvirt "github.com/libvirt/libvirt-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"sync"
)

// globals

var file *os.File
var connectURI *string

// structure for label values
type LabelVal struct {
	NovaName    string
	LibVirtName string
	NovaProject string
}

// structures for XML parsing

type Domain struct {
	XMLName  xml.Name `xml:"domain"`
	Type     string   `xml:"type,attr"`
	Name     string   `xml:"name"`
	Instance Instance `xml:"metadata"`
}

type Instance struct {
	Name  string `xml:"instance>name"`
	Owner Owner  `xml:"instance>owner"`
}
type Owner struct {
	Project string `xml:"project"`
}

// structure for channel return to use for metrics and tags

type Result struct {
	CpuTime     float64
	CpuNumber   float64
	UsedMemory  float64
        UsedMemoryCache float64
	AvailMemory float64
	NovaName    string
	LibVirtName string
	NovaProject string
	DomError    error
}

// define error structure for channel return and errors for the custom error interface implementation

type CustomError struct {
	Context string
	Err     error
}

// set up structure for prometheus descriptors

type metricsCollector struct {
	cpuUsage *prometheus.Desc
	memUsage *prometheus.Desc
	memUsageCache *prometheus.Desc
	cpuNum   *prometheus.Desc
	memAlloc *prometheus.Desc
}

// truncate large error logs

func fileCheck() {
	f, err := file.Stat()
	if err != nil {
		fmt.Println("cannot read log file")
		os.Exit(0)
	}
	if f.Size() > 2000000 {
		_, err := file.Seek(0, 0)
		if err != nil {
			fmt.Println("cannot read log file")
			os.Exit(0)
		}
		err = file.Truncate(2000000)
		if err != nil {
			fmt.Println("cannot read log file")
			os.Exit(0)
		}
		_, err = file.Seek(0, 2)
		if err != nil {
			fmt.Println("cannot read log file")
			os.Exit(0)
		}

		errs := Wrap(err, "Too Many Errors, truncated")
		log.Println(errs)
	}

}

// initialise descriptors

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{
		cpuUsage: prometheus.NewDesc(
			"libvirt_nova_instance_cpu_time_total",
			"instance vcpu time",
			[]string{"novaname", "libvirtname", "novaproject"}, nil,
		),
		memUsage: prometheus.NewDesc(
			"libvirt_nova_instance_memory_used_kb",
			"instance memory used without buffers/cache",
			[]string{"novaname", "libvirtname", "novaproject"}, nil,
		),
		memUsageCache: prometheus.NewDesc(
			"libvirt_nova_instance_memory_cache_used_kb",
			"instance memory used including buffers/cache",
			[]string{"novaname", "libvirtname", "novaproject"}, nil,
		),
		cpuNum: prometheus.NewDesc(
			"libvirt_nova_instance_vcpu_count",
			"instance vcpu allocated",
			[]string{"novaname", "libvirtname", "novaproject"}, nil,
		),
		memAlloc: prometheus.NewDesc(
			"libvirt_nova_instance_memory_alloc_kb",
			"instance memory allocated",
			[]string{"novaname", "libvirtname", "novaproject"}, nil,
		),
	}
}

// As per in Collector interface there is a describe method
// https://godoc.org/github.com/prometheus/client_golang/prometheus#Collector

func (collecting *metricsCollector) Describe(ch chan<- *prometheus.Desc) {

	//	prometheus.DescribeByCollect(collecting, ch)
	ch <- collecting.cpuUsage
	ch <- collecting.memUsage
	ch <- collecting.memUsageCache
	ch <- collecting.cpuNum
	ch <- collecting.memAlloc
}

// Collect method as per Collector interface. Here we populate the metrics with numbers

func (collecting *metricsCollector) Collect(ch chan<- prometheus.Metric) {

	conn, err := libvirt.NewConnectReadOnly(*connectURI)
	if err != nil {
		err = Wrap(err, "NewConnectReadOnly")
		log.Fatal(err)
	}
	defer conn.Close()

	// get all active domains
	doms, err := conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE)
	//doms, err := conn.ListAllDomains()
	if err != nil {
		err = Wrap(err, "ListAllDomains")
		log.Fatal(err)
	}

	// set up channel with Result and CustonError structure
	out := make(chan Result, len(doms))
	errchan := make(chan CustomError, (len(doms) * 8))

	// set up waitgroup so we wait for all goroutines
	// for all domains to finish before getting data
	var wg sync.WaitGroup
	// parallel goroutines called to get cpu and
	// memory stats for each domain. Pass it a slice of all domains
	for _, dom := range doms {
		wg.Add(1)
		dom := dom
		go getStats(dom, out, &wg, errchan)
	}

	wg.Wait()
	close(out)
	close(errchan)
	// check for errors and log
	for e := range errchan {
		errs := Wrap(e.Err, e.Context)
		log.Println(errs)
		fileCheck()
	}
	for i := range out {
		ch <- prometheus.MustNewConstMetric(collecting.cpuUsage, prometheus.CounterValue, i.CpuTime, i.NovaName, i.LibVirtName, i.NovaProject)
		ch <- prometheus.MustNewConstMetric(collecting.memUsage, prometheus.GaugeValue, i.UsedMemory, i.NovaName, i.LibVirtName, i.NovaProject)
		ch <- prometheus.MustNewConstMetric(collecting.memUsageCache, prometheus.GaugeValue, i.UsedMemoryCache, i.NovaName, i.LibVirtName, i.NovaProject)
		ch <- prometheus.MustNewConstMetric(collecting.cpuNum, prometheus.GaugeValue, i.CpuNumber, i.NovaName, i.LibVirtName, i.NovaProject)
		ch <- prometheus.MustNewConstMetric(collecting.memAlloc, prometheus.GaugeValue, i.AvailMemory, i.NovaName, i.LibVirtName, i.NovaProject)
	}

}

// goroutine launched in parallel to get metrics

func getStats(dom libvirt.Domain, out chan<- Result, wg *sync.WaitGroup, errchan chan<- CustomError) {

	var vcput uint64
	var availablemem uint64
	var unusedmem uint64
	var usablemem uint64
	errcount := 0

	// for wait group to inform when all goroutines done
	defer wg.Done()

	// get domain name for errors
	domName, err := dom.GetName()
	if err != nil {
		errchan <- CustomError{Err: err, Context: "GetName from getStats goroutine"}
		errcount++
	}
	// set up test error checks - change to err!=nil to run tests
	err = doRequest()
	if err == nil {
		errchan <- CustomError{Err: err, Context: domName + " : Test error message from getStats goroutine"}
		errcount++
	}

	// get domain cpu nanoseconds
	name, err := dom.GetCPUStats(-1, 1, 0)
	if err != nil {
		errchan <- CustomError{Err: err, Context: domName + " : GetCPUStats from getStats goroutine"}
		errcount++
	}
	for _, data := range name {
		vcput = data.CpuTime
	}
	// get number of vcpus
	ncpu, err := dom.GetMaxVcpus()
	if err != nil {
		errchan <- CustomError{Err: err, Context: domName + " : GetMaxvcpus from getStats goroutine"}
		errcount++
	}
	// get domain memory used
	// from virDomainMemoryStatTags https://libvirt.org/html/libvirt-libvirt-domain.html

	// The total amount of usable memory as seen by the domain. This value 
	// may be less than the amount of memory assigned to the domain if a 
	// balloon driver is in use or if the guest OS does not initialize all assigned pages. 
	// This value is expressed in kB.
	mema := libvirt.DOMAIN_MEMORY_STAT_AVAILABLE

	// How much the balloon can be inflated without pushing the guest 
	// system to swap, corresponds to 'Available' in /proc/meminfo

	memu := libvirt.DOMAIN_MEMORY_STAT_USABLE
	// The amount of memory left completely unused by the system. 
	// Memory that is available but used for reclaimable caches 
	// should NOT be reported as free. This value is expressed in kB.
	memud := libvirt.DOMAIN_MEMORY_STAT_UNUSED

	memory, err := dom.MemoryStats(12, 0)
	if err == nil {

		for _, data := range memory {
			if data.Tag == int32(mema) {
				availablemem = data.Val
			}

			if data.Tag == int32(memu) {
				usablemem = data.Val
			}

			if data.Tag == int32(memud) {
				unusedmem = data.Val
			}

		}
	} else {
		errchan <- CustomError{Err: err, Context: domName + " : MemoryStats from getStats goroutine"}
		errcount++
	}
	usednocache := (availablemem - usablemem)
	usedaddcache := (availablemem - unusedmem)
	//get domain xml nova descriptions
	xmldoc, err := dom.GetXMLDesc(0)
	m := &Domain{}
	err = xml.Unmarshal([]byte(xmldoc), &m)
	if err != nil {

		errchan <- CustomError{Err: err, Context: domName + " : GetXMLDesc from getStats goroutine"}
		errcount++
	}
	// "it is neccessary to explicitly release the reference at the Go level.
	// e.g. if a Go method returns a '* Domain' struct, it is
	// neccessary to call 'Free' on this when no longer required."
	// https://godoc.org/github.com/libvirt/libvirt-go
	err = dom.Free()
	if err != nil {
		errchan <- CustomError{Err: err, Context: domName + " : DomFree from getStats goroutine"}
		errcount++
	}
	if errcount == 0 {
		// populate Result structure and pass back to channel

		out <- Result{NovaName: m.Instance.Name, NovaProject: m.Instance.Owner.Project, LibVirtName: m.Name, CpuNumber: float64(ncpu), CpuTime: float64(vcput), AvailMemory: float64(availablemem), UsedMemory: float64(usednocache), UsedMemoryCache: float64(usedaddcache)}
	} else {
		errchan <- CustomError{Err: err, Context: domName + " : Many errors from getStats goroutine so skipping this iteration"}
		out <- Result{}
	}

}

//test error generation
func doRequest() error {
	return errors.New("this is a test error")
}

// custom error output using the error interface
func (w *CustomError) Error() string {
	return fmt.Sprintf("%s: %v", w.Context, w.Err)
}

//returm error message in CustomError format
func Wrap(err error, info string) *CustomError {
	return &CustomError{
		Context: info,
		Err:     err,
	}
}

func main() {

	// get listening port and logfile dir
	promPort := flag.String("port", "9100", "Port to listen on")
	logPath := flag.String("log", "./libvirt_nova.log", "path to log file")
	connectURI = flag.String("uri", "qemu:///system", "libvirt connect uri")
	scrapePath := flag.String("path", "/metrics", "path to expose metrics")
	flag.Parse()
	// set up logging

	fileNew, err := os.OpenFile(*logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer fileNew.Close()
	log.SetOutput(fileNew)
	file = fileNew

	fmt.Println("Prometheus libvirt exporter for Openstack Nova")
	log.Print("Started successfully")

	// record metrics
	foo := newMetricsCollector()
	prometheus.MustRegister(foo)
	// set up prometheus http service

	http.Handle(*scrapePath, promhttp.Handler())
	log.Fatal(http.ListenAndServe(":"+*promPort, nil))
}
