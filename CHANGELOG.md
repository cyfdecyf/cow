## 更新说明
- 2016-09-29 Version 1.5

       * 更新中国IP列表

- 2016-02-18 Version 1.3.4

       * 使用 Go 1.6 编译，请重新下载
       
- 2015-12-03 Version 1.3.4

       * 修正客户端连接未正确关闭 bug
       * 修正对文件描述符过多错误的判断（too many open files）

- 2015-11-22 Version 1.3.3

       * 增加 `reject` 拒绝连接列表
       * 支持作为 HTTPS 代理服务器监听
       * 支持 HTTPS 代理服务器作为父代理
	
	
- 2015-10-09 Version 1.3.2

       * 完全托管在 github，不再使用 meowproxy.me 域名，[新的下载地址](https://github.com/renzhn/MEOW/tree/gh-pages/dist/)

- 2015-08-23 Version 1.3.1

       * 去除了端口限制
       * 使用最新的 Go 1.5 编译

- 2015-07-16 Version 1.3

       更新了默认的直连列表、加入了强制使用代理列表，强烈推荐旧版本用户更新 [direct](https://raw.githubusercontent.com/renzhn/MEOW/master/doc/sample-config/direct) 文件和下载 [proxy](https://raw.githubusercontent.com/renzhn/MEOW/master/doc/sample-config/proxy) 文件（或者重新安装）
