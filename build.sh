# 构建登录工具
GOOS=darwin GOARCH=arm64 go build -o xiaohongshu-login ./cmd/login/

# 使用登录工具获取 知乎 cookie
./xiaohongshu-login -platform zhihu

# 构建 MCP 服务
GOOS=darwin GOARCH=arm64 go build -o xiaohongshu-mcp .

# 启动MCP服务
./xiaohongshu-mcp