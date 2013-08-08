export GOPATH=$(shell echo $$PWD)

GOBINS = dir_watcher

all:: get build
PHONY: get build

get:
	@go get $(GOBINS)

build:
	@go install $(GOBINS)
