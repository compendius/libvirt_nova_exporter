# Libvirt Nova Exporter
Prometheus libvirt exporter for Openstack Nova instances


### What is this?

This is some code I have written in Go that aims to export a very small subset of metrics from libvirt for Openstack Nova instances.
The principal is to get real time usage information, for example how many vcpus are in use.
If you prefer Python I have also written a version in Python here [https://github.com/compendius/libvirt_nova_exporter_py](https://github.com/compendius/libvirt_nova_exporter_py)

### Build

To build the code on Linux you there is a Docker file to create an image which has the libvirt dependencies required.

```
docker build --tag golibvirt:1.0 . 
```


### Options  

 * ```-uri``` the libvirt URI to connect to -  default - ```qemu:///system```
 * ```-path``` the scrape target path  - default - ```/metrics```
 * ```-port``` port to listen on  - default - ```9100```
 * ```-log``` log file path  - default - working directory

#### Running in a Container

For example on port 9201

```
docker build --tag golibvirt:1.0 .
docker run --rm -d  --network host \ 
-v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro:ro \
-v "$(pwd)":/var/log/ \
golibvirt  -log /var/log/libvirt_nova.log -port 9201
```
### Code Mechanics

The code creates a custom collector and uses the Collect method (defined in the Collector interface) to get the metrics from libvirt. 
For each active instance in a libvirt Domain the code launches a new goroutine calling libvirt Go binding to the libvirt C library
to get metrics. Each API call for each metric has error trapping in a dedicated error channel which should appear in the log file.
Log files over 3MB will be truncated leaving most recent output.
When an instance is removed from Nova (and therefore libvirt) the custom Collector removes the related metric for that instance.

### Metrics


```
# HELP libvirt_nova_instance_cpu_time_total instance vcpu time
# TYPE libvirt_nova_instance_cpu_time_total counter
libvirt_nova_instance_cpu_time_total{libvirtname="instance-0000030f",novaname="cems-testing-0",novaproject="admin"} 2.57646967433e+12
# HELP libvirt_nova_instance_memory_alloc_kb instance memory allocated
# TYPE libvirt_nova_instance_memory_alloc_kb gauge
libvirt_nova_instance_memory_alloc_kb{libvirtname="instance-0000030f",novaname="cems-testing-0",novaproject="admin"} 489084
# HELP libvirt_nova_instance_memory_cache_used_kb instance memory used including buffers/cache
# TYPE libvirt_nova_instance_memory_cache_used_kb gauge
libvirt_nova_instance_memory_cache_used_kb{libvirtname="instance-0000030f",novaname="cems-testing-0",novaproject="admin"} 48980
# HELP libvirt_nova_instance_memory_used_kb instance memory used without buffers/cache
# TYPE libvirt_nova_instance_memory_used_kb gauge
libvirt_nova_instance_memory_used_kb{libvirtname="instance-0000030f",novaname="cems-testing-0",novaproject="admin"} 29436
# HELP libvirt_nova_instance_vcpu_count instance vcpu allocated
# TYPE libvirt_nova_instance_vcpu_count gauge
libvirt_nova_instance_vcpu_count{libvirtname="instance-0000030f",novaname="cems-testing-0",novaproject="admin"} 1
# HELP libvirt_nova_instance_rxbytes instance rxbytes
# TYPE libvirt_nova_instance_rxbytes counter
libvirt_nova_instance_rxbytes{iface="tap29b6117f-cf",libvirtname="instance-0000030f",macaddr="fa:16:3e:50:93:ec",novaname="cems
-testing-0",novaproject="admin"} 95561
# HELP libvirt_nova_instance_txbytes instance txbytes
# TYPE libvirt_nova_instance_txbytes counter
libvirt_nova_instance_txbytes{iface="tap29b6117f-cf",libvirtname="instance-0000030f",macaddr="fa:16:3e:50:93:ec",novaname="cems
-testing-0",novaproject="admin"} 0
```

### PromQL examples
#### How many vCPUs in use


```irate(libvirt_nova_instance_cpu_time_total{novaname=~"testing.*"}[30s])/1e+9```

Note that the ```scrape interval``` is 15s in this case defined in the Prometheus server config file,  
so here irate is effectively calculating the delta between two 15 second cpu time scrapes which should give you the vCPUs in use

[https://prometheus.io/docs/prometheus/latest/querying/functions/](https://prometheus.io/docs/prometheus/latest/querying/functions/)

"irate()
irate(v range-vector) calculates the per-second instant rate of increase of the time series in the range vector. This is based on the last two data points"


### Errors

Errors are somtimes logged when instances are removed from being 'active' in libvirt (deleted/shutdown etc) whilst the Collect() method is called. This is a natural consequence of calling a function on a libvirt domain
whilst the Domain is in the process of state change. These errors are for information only and include the relevant domain name. The relevant goroutine is terminated if errors like these are detected.

```
2020/11/24 12:24:20 instance-000002bc : MemoryStats from getStats goroutine: virError(Code=55, Domain=20, Message='Requested operation is not valid: domain is not running')
``` 
