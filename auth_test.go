package main

import (
	"testing"
)

func TestCalcDigest(t *testing.T) {
	a1 := md5sum("cyf" + ":" + authRealm + ":" + "wlx")
	auth := map[string]string{
		"nonce":  "50ed159c3b707061418bbb14",
		"nc":     "00000001",
		"cnonce": "6c46874228c087eb",
		"uri":    "/",
	}
	const targetDigest = "bad1cb3526e4b257a62cda10f7c25aad"

	digest := calcRequestDigest(auth, a1, "GET")
	if digest != targetDigest {
		t.Errorf("authentication digest calculation wrong, got: %x, should be: %s\n", digest, targetDigest)
	}
}
