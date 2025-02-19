.PHONY: dev build run

dev:
	air

build:
	go build -o tmp/main

run:
	go run .
