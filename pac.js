var direct = 'DIRECT';
var httpProxy = 'PROXY';

var directList = [
	"", // corresponds to simple host name
	"taobao.com",
	"www.baidu.com",
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

var topLevel = {
        "net": true,
        "org": true,
        "edu": true,
        "com": true,
        "ac": true,
        "co": true
};

// only handles IPv4 address now
function hostIsIP(host) {
	var parts = host.split('.');
	if (parts.length != 4) {
		return false;
	}
	for (var i = 3; i >= 0; i--) {
		if (parts[i].length == 0 || parts[i].length > 3) {
			return false
		}
		var n = Number(parts[i])
		if (isNaN(n) || n < 0 || n > 255) {
			return false;
		}
	}
	return true;
}

function host2domain(host) {
	var lastDot = host.lastIndexOf('.');
	if (lastDot === -1) {
		return ""; // simple host name has no domain
	}
	// Find the second last dot
	dot2ndLast = host.lastIndexOf(".", lastDot-1);
	if (dot2ndLast === -1)
		return host;

	var part = host.substring(dot2ndLast+1, lastDot)
	if (topLevel[part]) {
		var dot3rdLast = host.lastIndexOf(".", dot2ndLast-1)
		if (dot3rdLast === -1) {
			return host;
		}
		return host.substring(dot3rdLast+1);
	}
	return host.substring(dot2ndLast+1);
};

function FindProxyForURL(url, host) {
	return (hostIsIP(host) || directAcc[host] || directAcc[host2domain(host)]) ? direct : httpProxy;
};

// Tests

if (FindProxyForURL("", "192.168.1.1") != direct) {
	console.log("ip should return direct");
}
if (FindProxyForURL("", "localhost") != direct) {
	console.log("localhost should return direct");
}
if (FindProxyForURL("", "simple") != direct) {
	console.log("simple host name should return direct");
}
if (FindProxyForURL("", "taobao.com") != direct) {
	console.log("taobao.com should return direct");
}
if (FindProxyForURL("", "www.taobao.com") != direct) {
	console.log("www.taobao.com should return direct");
}
if (FindProxyForURL("", "www.baidu.com") != direct) {
	console.log("www.baidu.com should return direct");
}
if (FindProxyForURL("", "baidu.com") != httpProxy) {
	console.log("baidu.com should return proxy");
}
if (FindProxyForURL("", "google.com") != httpProxy) {
	console.log("google.com should return proxy");
}

if (hostIsIP("192.168.1.1") !== true) {
	console.log("192.168.1.1 is ip");
}
if (hostIsIP("google.com") === true) {
	console.log("google.com is not ip");
}
