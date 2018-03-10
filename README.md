# rely

port of reliable.io to Go

[godoc](https://godoc.org/github.com/jakecoffman/rely)

# performance

Performance on Linux is just a little worse than the C version without pooling memory:

```
$ premake5 soak && /usr/bin/time -v ./soak 8100
	Command being timed: "./bin/soak 8100"
	User time (seconds): 1.48
	System time (seconds): 0.00
	Percent of CPU this job got: 99%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.48
	Average shared text size (kbytes): 0
	Average unshared data size (kbytes): 0
	Average stack size (kbytes): 0
	Average total size (kbytes): 0
	Maximum resident set size (kbytes): 2056
	Average resident set size (kbytes): 0
	Major (requiring I/O) page faults: 0
	Minor (reclaiming a frame) page faults: 379
	Voluntary context switches: 1
	Involuntary context switches: 129
	Swaps: 0
	File system inputs: 0
	File system outputs: 0
	Socket messages sent: 0
	Socket messages received: 0
	Signals delivered: 0
	Page size (bytes): 4096
	Exit status: 0
```

```
$ go build -tags=test && /usr/bin/time -v ./soak -iterations=8100
	Command being timed: "./soak -iterations=8100"
	User time (seconds): 1.74
	System time (seconds): 0.03
	Percent of CPU this job got: 105%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.67
	Average shared text size (kbytes): 0
	Average unshared data size (kbytes): 0
	Average stack size (kbytes): 0
	Average total size (kbytes): 0
	Maximum resident set size (kbytes): 8616
	Average resident set size (kbytes): 0
	Major (requiring I/O) page faults: 0
	Minor (reclaiming a frame) page faults: 1606
	Voluntary context switches: 1689
	Involuntary context switches: 234
	Swaps: 0
	File system inputs: 0
	File system outputs: 0
	Socket messages sent: 0
	Socket messages received: 0
	Signals delivered: 0
	Page size (bytes): 4096
	Exit status: 0
```