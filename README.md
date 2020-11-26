# Libvirt Nova Exporter
Prometheus libvirt exporter for Openstack Nova instances


### Whats is this?

This is some code I have written in Go that aims to export a very small subset of metrics from libvirt for Openstack Nova instances.
The principal is to get real time usage information, for example how many vcpus are in use.


### Build

To build the code on Linux you there is a Docker file to create an image which has the libvirt dependencies required.

```
docker build --tag golibvirt:1.0 .
./buildme.sh 
```
 I have provided a binary that should work on Linux

### Run

For testing you can run as below, but it is recommended to create a systemd service

```libvirt_nova -port 9200 -log /var/log/libvirt_nova.log &```

Options - 

 * ```-uri``` the libvirt URI to connect to -  default - ```qemu:///system```
 * ```-path``` the scrape target path  - default - ```/metrics```
 * ```-port``` port to listen on  - default - ```9100```
 * ```-log``` log file path  - default - working directory

### Code Mechanics

For each active instance in a libvirt Domain the code launches a new goroutine calling libvirt Go binding to the libvirt C library
to get metrics. This promotes good concurrency. Each API call for each metric has error trapping in a dedicated error channel which should appear in the log file.
Log files over 3MB will be truncated leaving most recent output.
When an instance is removed from Nova (and therefore libvirt) the code removes removes the related metric for that instance.
The code polls for metrics every second.

### Metrics

Note that metric ```libvirt_nova_instance_cpu_time_total``` is a counter not a gauge. This means it will work properly with PromQL rate() and irate() 
which takes into account counter resets.

```
# HELP libvirt_nova_instance_cpu_time_total instance vcpu time
# TYPE libvirt_nova_instance_cpu_time_total counter
libvirt_nova_instance_cpu_time_total{libvirtname="instance-00000037",novaname="testing-2",novaproject="admin"} 1.7676361e+07
libvirt_nova_instance_cpu_time_total{libvirtname="instance-00000038",novaname="testing-1",novaproject="admin"} 2.7172637e+07
# HELP libvirt_nova_instance_memory_alloc_kb instance memory allocated
# TYPE libvirt_nova_instance_memory_alloc_kb gauge
libvirt_nova_instance_memory_alloc_kb{libvirtname="instance-00000037",novaname="testing-2",novaproject="admin"} 489084
libvirt_nova_instance_memory_alloc_kb{libvirtname="instance-00000038",novaname="testing-1",novaproject="admin"} 489084
# HELP libvirt_nova_instance_memory_used_kb instance memory used
# TYPE libvirt_nova_instance_memory_used_kb gauge
libvirt_nova_instance_memory_used_kb{libvirtname="instance-00000037",novaname="testing-2",novaproject="admin"} 29492
libvirt_nova_instance_memory_used_kb{libvirtname="instance-00000038",novaname="testing-1",novaproject="admin"} 29444
# HELP libvirt_nova_instance_vcpu_count instance vcpu allocated
# TYPE libvirt_nova_instance_vcpu_count gauge
libvirt_nova_instance_vcpu_count{libvirtname="instance-00000037",novaname="testing-2",novaproject="admin"} 1
libvirt_nova_instance_vcpu_count{libvirtname="instance-00000038",novaname="testing-1",novaproject="admin"} 1
```

### PromQL examples
#### How many vCPUs in use

```irate(libvirt_nova_instance_cpu_time_total{novaname=~"testing.*"}[1m])/1e+9```

"irate()
irate(v range-vector) calculates the per-second instant rate of increase of the time series in the range vector. This is based on the last two data points"

[https://prometheus.io/docs/prometheus/latest/querying/functions/](https://prometheus.io/docs/prometheus/latest/querying/functions/)

### Errors

Errors are logged when instances are removed from being 'active' in libvirt (deleted/shutdown etc). This is a natural consequence of calling a function on a libvirt domain
whilst the Domain is in the process of state change. These errors are for information only and include the relevant domain name. The relevant goroutine is terminated if errors like these are detected.

```
2020/11/24 12:24:20 instance-000002bc : MemoryStats from getStats goroutine: virError(Code=55, Domain=20, Message='Requested operation is not valid: domain is not running')
``` 
### Running in a Container

Sometimes it may be convenient to run in a container listening to libvirt on the host

For example on port 9201

```
cd docker
docker build --tag golibvirtdocker:1.0 .
docker run --rm -d  --network host \ 
-v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro:ro \
-v "$(pwd)":/var/log/ \
golibvirtdocker  -log /var/log/libvirt_nova.log -port 9201
```
