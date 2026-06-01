mkdir -p dist
GOOS=darwin GOARCH=amd64 go build -o dist/yttomp3-macos .
