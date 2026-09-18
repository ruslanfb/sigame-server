package lan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinURLs(t *testing.T) {
	addrs := []Address{{IP: "192.168.1.5", Private: true}, {IP: "10.0.0.2", Private: true}, {IP: "fd00::1", IsIPv6: true, Private: true}}
	t.Run("wildcard listen lists every address", func(t *testing.T) {
		urls := JoinURLs(":8080", "", addrs)
		require.Equal(t, []string{"http://192.168.1.5:8080", "http://10.0.0.2:8080", "http://[fd00::1]:8080"}, urls)
	})
	t.Run("explicit host only", func(t *testing.T) {
		urls := JoinURLs("192.168.1.5:8080", "", addrs)
		require.Equal(t, []string{"http://192.168.1.5:8080"}, urls)
	})
	t.Run("public url first", func(t *testing.T) {
		urls := JoinURLs(":8080", "https://quiz.example.com", addrs[:1])
		require.Equal(t, []string{"https://quiz.example.com", "http://192.168.1.5:8080"}, urls)
	})
	t.Run("bad listen addr", func(t *testing.T) {
		require.Empty(t, JoinURLs("nonsense", "", addrs))
	})
}

func TestAddressesDoesNotFail(t *testing.T) {
	addrs, err := Addresses()
	require.NoError(t, err)
	for _, a := range addrs {
		require.NotEmpty(t, a.IP)
		require.NotEqual(t, "127.0.0.1", a.IP)
	}
}

func TestBanner(t *testing.T) {
	s := Banner([]string{"http://192.168.1.5:8080"})
	require.Contains(t, s, "http://192.168.1.5:8080/docs")
}
