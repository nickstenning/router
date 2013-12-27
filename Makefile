default: run

package = router

.PHONY: default run

clean:
	rm -rf `pwd`/gopath.tmp

setup:
	mkdir -p `pwd`/gopath.tmp/src/github.com/alphagov
	ln -s `pwd` `pwd`/gopath.tmp/src/github.com/alphagov/router

run:
	GOPATH=`pwd`/vendor:`pwd`/gopath.tmp go run -race $(package).go

test: build
	GOPATH=`pwd`/vendor:`pwd`/gopath.tmp go test ./trie ./triemux

build: setup
	GOPATH=`pwd`/vendor:`pwd`/gopath.tmp go build -v -o $(package)
