# rely

port of reliable.io to Go

[godoc](https://godoc.org/github.com/jakecoffman/rely)

# performance

Tests below done on Ubuntu with Go 1.10. For whatever reasons when I run the pooled test on a mac it runs in .34s so YMMV.

rely without pooling
```
$ go build -tags=test && /usr/bin/time -v ./soak -iterations=8100
	Command being timed: "./soak -iterations=8100"
	User time (seconds): 0.78
	System time (seconds): 0.01
	Percent of CPU this job got: 105%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 0:00.75
	Average shared text size (kbytes): 0
	Average unshared data size (kbytes): 0
	Average stack size (kbytes): 0
	Average total size (kbytes): 0
	Maximum resident set size (kbytes): 8360
	Average resident set size (kbytes): 0
	Major (requiring I/O) page faults: 0
	Minor (reclaiming a frame) page faults: 1488
	Voluntary context switches: 848
	Involuntary context switches: 111
	Swaps: 0
	File system inputs: 0
	File system outputs: 0
	Socket messages sent: 0
	Socket messages received: 0
	Signals delivered: 0
	Page size (bytes): 4096
	Exit status: 0
```

rely with pooling
```
$ go build -tags=test && /usr/bin/time -v ./soak -iterations=8100 -pool=true
	Command being timed: "./soak -iterations=8100 -pool=true"
	User time (seconds): 0.66
	System time (seconds): 0.00
	Percent of CPU this job got: 103%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 0:00.64
	Average shared text size (kbytes): 0
	Average unshared data size (kbytes): 0
	Average stack size (kbytes): 0
	Average total size (kbytes): 0
	Maximum resident set size (kbytes): 8204
	Average resident set size (kbytes): 0
	Major (requiring I/O) page faults: 0
	Minor (reclaiming a frame) page faults: 1472
	Voluntary context switches: 373
	Involuntary context switches: 72
	Swaps: 0
	File system inputs: 0
	File system outputs: 0
	Socket messages sent: 0
	Socket messages received: 0
	Signals delivered: 0
	Page size (bytes): 4096
	Exit status: 0
```

reliable.io

```
$ /usr/bin/time -v ./bin/soak 8100
	Command being timed: "./bin/soak 8100"
	User time (seconds): 0.35
	System time (seconds): 0.00
	Percent of CPU this job got: 99%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 0:00.35
	Average shared text size (kbytes): 0
	Average unshared data size (kbytes): 0
	Average stack size (kbytes): 0
	Average total size (kbytes): 0
	Maximum resident set size (kbytes): 1960
	Average resident set size (kbytes): 0
	Major (requiring I/O) page faults: 0
	Minor (reclaiming a frame) page faults: 379
	Voluntary context switches: 1
	Involuntary context switches: 32
	Swaps: 0
	File system inputs: 0
	File system outputs: 0
	Socket messages sent: 0
	Socket messages received: 0
	Signals delivered: 0
	Page size (bytes): 4096
	Exit status: 0
```
