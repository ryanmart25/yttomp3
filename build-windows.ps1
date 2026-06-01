mkdir -p dist
GOOS=windows GOARCH=amd64 go build -o dist\yttomp3-windows.exe .
