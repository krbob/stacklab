package backup

import (
	"context"
	"testing"
)

type listingRunner struct{ output []byte }

func (r listingRunner) Run(context.Context, string, []string, []string, string) ([]byte, error) {
	return r.output, nil
}
func TestSelectedRestoreSpaceUsesRepositoryObjectsInsteadOfWholeSnapshot(t *testing.T) {
	s, _, _ := newTestService(t)
	destination := addDestination(t, s, "Selected")
	s.runner = listingRunner{output: []byte(`{"struct_type":"snapshot","id":"aabbccdd"}
{"struct_type":"node","path":"/manifest.json","type":"file","size":128}
{"struct_type":"node","path":"/sources/small.tar","type":"file","size":10240}
{"struct_type":"node","path":"/indexes/small.json","type":"file","size":256}
{"struct_type":"node","path":"/sources/huge.tar","type":"file","size":1000000000000000}
`)}
	size, err := s.restoreSize(context.Background(), destination, "aabbccdd", []string{"/manifest.json", "/sources/small.tar", "/indexes/small.json"})
	if err != nil {
		t.Fatal(err)
	}
	if size != 128+10240+256 {
		t.Fatalf("selected size=%d", size)
	}
	if _, err = s.restoreSize(context.Background(), destination, "aabbccdd", []string{"/sources/missing.tar"}); err == nil {
		t.Fatal("missing selected object accepted")
	}
	s.runner = listingRunner{output: []byte(`{"path":"/sources/small.tar","type":"symlink","size":1}`)}
	if _, err = s.restoreSize(context.Background(), destination, "aabbccdd", []string{"/sources/small.tar"}); err == nil {
		t.Fatal("linked archive accepted")
	}
}
