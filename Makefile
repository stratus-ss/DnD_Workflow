.PHONY: build build-statigo setup-statigo install test clean tidy

build:
	go build -ldflags="-s -w" -o dnd-workflow ./cmd/dnd-workflow

build-statigo: setup-statigo
	CGO_ENABLED=1 go build -tags statigo -ldflags="-s -w" -o dnd-workflow ./cmd/dnd-workflow

setup-statigo:
	@test -d third_party/ffmpeg-statigo || git submodule add https://github.com/linuxmatters/ffmpeg-statigo third_party/ffmpeg-statigo
	cd third_party/ffmpeg-statigo && go run ./cmd/download-lib
	@grep -q 'replace github.com/linuxmatters/ffmpeg-statigo' go.mod || \
		echo 'replace github.com/linuxmatters/ffmpeg-statigo => ./third_party/ffmpeg-statigo' >> go.mod

install: build
	cp dnd-workflow /usr/local/bin/

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f dnd-workflow
	rm -rf output/
