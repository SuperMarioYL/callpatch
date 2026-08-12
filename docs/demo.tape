Output assets/demo.gif
Set FontSize 14
Set Width 1000
Set Height 460
Set Padding 16
Set Theme "Dracula"
Set TypingSpeed 28ms

# Build the binary once, then replay two recorded failure traces.
Sleep 500ms
Type "$ go build -o /tmp/callpatch ./cmd/callpatch"
Sleep 200ms
Enter
Sleep 3s

# Qwen3: a ```json fence envelope whose closing fence + brace were lost mid-stream.
Type "$ /tmp/callpatch repair --trace testdata/qwen3-broken.sse"
Sleep 200ms
Enter
Sleep 3s

Type "clear"
Sleep 150ms
Enter
Sleep 800ms

# DeepSeek-V4: a sentinel run leaked into the arguments JSON + truncation.
Type "$ /tmp/callpatch repair --trace testdata/deepseek-broken.sse"
Sleep 200ms
Enter
Sleep 3s
Sleep 1s
