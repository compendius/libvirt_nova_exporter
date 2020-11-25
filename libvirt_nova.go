// Written by Charles Short
// 17/11/2020
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
	"time"
)

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

// set up prometheus variables

var (
	cpuUsage = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "libvirt_nova_instance_cpu_time_total",
		Help: "instance vcpu time",
	},
		[]string{"novaname", "libvirtname", "novaproject"},
	)
	memUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "libvirt_nova_instance_memory_used_kb",
		Help: "instance memory used",
	},
		[]string{"novaname", "libvirtname", "novaproject"},
	)
	cpuNum = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "libvirt_nova_instance_vcpu_count",
		Help: "instance vcpu allocated",
	},
		[]string{"novaname", "libvirtname", "novaproject"},
	)
	memAlloc = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "libvirt_nova_instance_memory_alloc_kb",
		Help: "instance memory allocated",
	},
		[]string{"novaname", "libvirtname", "novaproject"},
	)
)

func recordMetrics(conn *libvirt.Connect, file *os.File) {

	var cpucounter float64
	cpuhist := make(map[LabelVal]float64)

	for {

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
		// temp map for cpu stats
		cpuhisttemp := make(map[LabelVal]float64)

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
			// manage large log file and truncate
			// from top if too large
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

				errs = Wrap(err, "Too Many Errors, truncated")
				log.Println(errs)
			}
		}
		// looks at return channel struct and build metrics
		for i := range out {
			// set up cpu seconds map for incremental comparison with previous metric.
			// Prometheus counters must start at 0 and go up
			// so this works out the difference and there are only Inc()
			// and Add() methods. Counter manages resets
			// https://godoc.org/github.com/prometheus/client_golang/prometheus#Counter
			promLab := LabelVal{NovaName: i.NovaName, LibVirtName: i.LibVirtName, NovaProject: i.NovaProject}

			if _, ok := cpuhist[promLab]; ok {
				cpucounter = (i.CpuTime - cpuhist[promLab])
			} else {
				cpucounter = 0

			}
			cpuhisttemp[promLab] = i.CpuTime

			cpuUsage.WithLabelValues(i.NovaName, i.LibVirtName, i.NovaProject).Add(cpucounter)
			cpuNum.WithLabelValues(i.NovaName, i.LibVirtName, i.NovaProject).Set(i.CpuNumber)
			memUsage.WithLabelValues(i.NovaName, i.LibVirtName, i.NovaProject).Set(i.UsedMemory)
			memAlloc.WithLabelValues(i.NovaName, i.LibVirtName, i.NovaProject).Set(i.AvailMemory)
		}

		//remove instances and clear temp map
		for k, _ := range cpuhist {
			if _, ok := cpuhisttemp[k]; ok {
				continue
			} else {

				cpuUsage.DeleteLabelValues(k.NovaName, k.LibVirtName, k.NovaProject)
				memUsage.DeleteLabelValues(k.NovaName, k.LibVirtName, k.NovaProject)
				memAlloc.DeleteLabelValues(k.NovaName, k.LibVirtName, k.NovaProject)
				cpuNum.DeleteLabelValues(k.NovaName, k.LibVirtName, k.NovaProject)

			}
		}
		cpuhist = cpuhisttemp

		// a one second delay between metric samples

		time.Sleep(time.Second)

	}
}

//test error generation
func doRequest() error {
	return errors.New("this is a test error")
}

// goroutine launched in parallel to get metrics

func getStats(dom libvirt.Domain, out chan<- Result, wg *sync.WaitGroup, errchan chan<- CustomError) {

	var vcput uint64
	var availablemem uint64
	var unusedmem uint64
	var usedmem uint64
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
	mema := libvirt.DOMAIN_MEMORY_STAT_AVAILABLE
	memu := libvirt.DOMAIN_MEMORY_STAT_USABLE
	memory, err := dom.MemoryStats(12, 0)
	if err == nil {

		for _, data := range memory {
			if data.Tag == int32(mema) {
				availablemem = data.Val
			}

			if data.Tag == int32(memu) {
				unusedmem = data.Val
			}

		}
	} else {
		errchan <- CustomError{Err: err, Context: domName + " : MemoryStats from getStats goroutine"}
		errcount++
	}
	usedmem = (availablemem - unusedmem)
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

		out <- Result{NovaName: m.Instance.Name, NovaProject: m.Instance.Owner.Project, LibVirtName: m.Name, CpuNumber: float64(ncpu), CpuTime: float64(vcput), AvailMemory: float64(availablemem), UsedMemory: float64(usedmem)}
	} else {
		errchan <- CustomError{Err: err, Context: domName + " : Many errors from getStats goroutine so skipping this iteration"}
		out <- Result{}
	}

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

func init() {
	// Metrics have to be registered to be exposed:
	//	reg := prometheus.NewRegistry()
	//	prometheus.MustRegister(cpuTemp)
	prometheus.MustRegister(cpuUsage)
	prometheus.MustRegister(memUsage)
	prometheus.MustRegister(cpuNum)
	prometheus.MustRegister(memAlloc)
}

func main() {

	// get listening port and logfile dir
	promPort := flag.String("port", "9100", "Port to listen on")
	logPath := flag.String("log", "./libvirt_nova.log", "path to log file")
	connectURI := flag.String("uri", "qemu:///system", "libvirt connect uri")
	scrapePath := flag.String("path", "/metrics", "path to expose metrics")
	flag.Parse()
	// set up logging

	file, err := os.OpenFile(*logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	log.SetOutput(file)

	// connect to libvirt

	conn, err := libvirt.NewConnectReadOnly(*connectURI)
	if err != nil {
		err = Wrap(err, "NewConnectReadOnly")
		log.Fatal(err)
	}
	defer conn.Close()
	fmt.Println("Prometheus libvirt exporter for Openstack Nova")
	log.Print("Started successfully")
	// record metrics

	go recordMetrics(conn, file)
	// set up prometheus http service

	http.Handle(*scrapePath, promhttp.Handler())
	log.Fatal(http.ListenAndServe(":"+*promPort, nil))
}
