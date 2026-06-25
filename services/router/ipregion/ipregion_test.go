package ipregion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIp2region_Search(t *testing.T) {
	region, err := NewIp2region("../ip2region.db")
	if err != nil {
		t.Skipf("skip: ip2region.db not available: %v", err)
	}

	got, err := region.Search("3.166.231.6")
	assert.Nil(t, err)
	t.Log(got)
}
