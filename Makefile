.PHONY: validate test race reference fuzz benchmark compare

validate:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; exit 1)
	go vet ./...

test:
	CGO_ENABLED=0 go test ./...

race:
	go test -race ./...

reference:
	GO_XDIFF_TESTREF=1 CGO_ENABLED=0 go test -count=1 ./...

fuzz:
	go test -run '^$$' -fuzz '^FuzzDiff$$' -fuzztime=15s -parallel=4 .
	go test -run '^$$' -fuzz '^FuzzMerge$$' -fuzztime=15s -parallel=4 .

benchmark:
	go test -run '^$$' -bench . -benchmem .

compare:
	bash benchmarks/run.sh
