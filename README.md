# rely

port of reliable.io to Go

[![GoDoc](https://godoc.org/github.com/jakecoffman/rely?status.svg)](http://godoc.org/github.com/jakecoffman/rely) [![Build Status](https://travis-ci.org/jakecoffman/rely.svg?branch=master)](https://travis-ci.org/jakecoffman/rely)

# performance

Tests below done on MBP 2.6GHz 6-Core i7 using Go 1.15.

rely without pooling
```
$ go build -tags=test && time ./soak -iterations=8100           
./soak -iterations=8100  0.26s user 0.02s system 99% cpu 0.280 total
```

rely with pooling
```
$ go build -tags=test && time ./soak -iterations=8100 -pool=true
./soak -iterations=8100 -pool=true  0.23s user 0.01s system 90% cpu 0.271 total
```

reliable.io

```
$ time ./bin/soak 8100
initializing
shutdown
./bin/soak 8100  0.70s user 0.00s system 63% cpu 1.124 total
```
