SHELL := /bin/bash
PREFIX ?= /usr/local

bin/treemouse: bin
	go build -o bin/treemouse

bin:
	[ -d "./bin" ] || mkdir bin

install: bin/treemouse
	[ -d "${PREFIX}/bin" ] || mkdir -p "${PREFIX}/bin"
	cp -pdf ./bin/treemouse ${PREFIX}/bin/treemouse

clean:
	rm -f ./bin/treemouse

.PHONY: install clean
