var direct = 'DIRECT';
var httpProxy = 'PROXY';

var directList = [
	"", // corresponds to simple host name and ip address
	"taobao.com",
	"www.baidu.com"
];

var directAcc = {};
for (var i = 0; i < directList.length; i += 1) {
	directAcc[directList[i]] = true;
}

var topLevel = {
        "ac": true,
        "co": true,
        "com": true,
        "edu": true,
        "gov": true,
        "net": true,
        "org": true
};

// hostIsIP determines whether a host address is an IP address and whether
// it is private. Currenly only handles IPv4 addresses.
function hostIsIP(host) {
	var part = host.split('.');
	if (part.length != 4) {
		return [false, false];
	}
	var n;
	for (var i = 3; i >= 0; i--) {
		if (part[i].length === 0 || part[i].length > 3) {
			return [false, false];
		}
		n = Number(part[i]);
		if (isNaN(n) || n < 0 || n > 255) {
			return [false, false];
		}
	}
	if (part[0] == '127' || part[0] == '10' || (part[0] == '192' && part[1] == '168')) {
		return [true, true];
	}
	if (part[0] == '172') {
		n = Number(part[1]);
		if (16 <= n && n <= 31) {
			return [true, true];
		}
	}
	return [true, false];
}

function host2Domain(host) {
	var arr, isIP, isPrivate;
	arr = hostIsIP(host);
	isIP = arr[0];
	isPrivate = arr[1];
	if (isPrivate) {
		return "";
	}
	if (isIP) {
		return host;
	}

	var lastDot = host.lastIndexOf('.');
	if (lastDot === -1) {
		return ""; // simple host name has no domain
	}
	// Find the second last dot
	dot2ndLast = host.lastIndexOf(".", lastDot-1);
	if (dot2ndLast === -1)
		return host;

	var part = host.substring(dot2ndLast+1, lastDot);
	if (topLevel[part]) {
		var dot3rdLast = host.lastIndexOf(".", dot2ndLast-1);
		if (dot3rdLast === -1) {
			return host;
		}
		return host.substring(dot3rdLast+1);
	}
	return host.substring(dot2ndLast+1);
}

function FindProxyForURL(url, host) {
	if (url.substring(0,4) == "ftp:")
		return direct;
	if (host.indexOf(".local", host.length - 6) !== -1) {
		return direct;
	}
	var domain = host2Domain(host);
	if (host.length == domain.length) {
		return directAcc[host] ? direct : httpProxy;
	}
	return (directAcc[host] || directAcc[domain]) ? direct : httpProxy;
}

// Tests

var testData, td, i;

testData = [
	{ ip: '127.0.0.1', isIP: true, isPrivate: true },
	{ ip: '127.2.1.1', isIP: true, isPrivate: true },
	{ ip: '192.168.1.1', isIP: true, isPrivate: true },
	{ ip: '172.16.1.1', isIP: true, isPrivate: true },
	{ ip: '172.20.1.1', isIP: true, isPrivate: true },
	{ ip: '172.31.1.1', isIP: true, isPrivate: true },
	{ ip: '172.15.1.1', isIP: true, isPrivate: false },
	{ ip: '172.32.1.1', isIP: true, isPrivate: false },
	{ ip: '10.16.1.1', isIP: true, isPrivate: true },
	{ ip: '12.3.4.5', isIP: true, isPrivate: false },
	{ ip: '1.2.3.4.5', isIP: false, isPrivate: false },
	{ ip: 'google.com', isIP: false, isPrivate: false },
	{ ip: 'www.google.com.hk', isIP: false, isPrivate: false }
];

for (i = 0; i < testData.length; i += 1) {
	td = testData[i];
	arr = hostIsIP(td.ip);
	if (arr[0] !== td.isIP) {
		if (td.isIP) {
			console.log(td.ip + " is ip");
		} else {
			console.log(td.ip + " is NOT ip");
		}
	}
	if (arr[0] !== td.isIP) {
		if (td.isIP) {
			console.log(td.ip + " is private ip");
		} else {
			console.log(td.ip + " is NOT private ip");
		}
	}
}

testData = [
	// private ip should return direct
	{ host: '192.168.1.1', mode: direct},
	{ host: '10.1.1.1', mode: direct},
	{ host: '172.16.2.1', mode: direct},
	{ host: '172.20.255.255', mode: direct},
	{ host: '172.31.255.255', mode: direct},
	{ host: '192.168.2.255', mode: direct},

	// simple host should return direct
	{ host: 'localhost', mode: direct},
	{ host: 'simple', mode: direct},

	// non private ip should return proxy
	{ host: '172.32.2.255', mode: httpProxy},
	{ host: '172.15.0.255', mode: httpProxy},
	{ host: '12.20.2.1', mode: httpProxy},

	// host in direct domain/host should return direct
	{ host: 'taobao.com', mode: direct},
	{ host: 'www.taobao.com', mode: direct},
	{ host: 'www.baidu.com', mode: direct},

	// host not in direct domain should return proxy
	{ host: 'baidu.com', mode: httpProxy},
	{ host: 'foo.baidu.com', mode: httpProxy},
	{ host: 'google.com', mode: httpProxy},
	{ host: 'www.google.com', mode: httpProxy},
	{ host: 'www.google.com.hk', mode: httpProxy},

	// host in local domain should return direct
	{ host: 'test.local', mode: direct},
	{ host: '.local', mode: direct},
];

for (i = 0; i < testData.length; i += 1) {
	td = testData[i];
	if (FindProxyForURL("", td.host) !== td.mode) {
		if (td.mode === direct) {
			console.log(td.host + " should return direct");
		} else {
			console.log(td.host + " should return proxy");
		}
	}
}
