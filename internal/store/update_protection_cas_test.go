package store

import (
	"sync"
	"testing"
	"time"
)

func TestAppSettingCompareAndSetPermitsOnlyOneWriterPerRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	key := StackUpdateProtectionKey("app")
	for _, stage := range []string{"insert", "update"} {
		_, revision, err := s.AppSettingWithRevision(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		results := make(chan bool, 2)
		errs := make(chan error, 2)
		var writers sync.WaitGroup
		for _, value := range []string{`{"auto_rollback":true}`, `{"auto_rollback":false}`} {
			writers.Go(func() {
				<-gate
				won, err := s.CompareAndSetAppSetting(ctx, key, value, revision, time.Now())
				results <- won
				errs <- err
			})
		}
		close(gate)
		writers.Wait()
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		if (<-results) == (<-results) {
			t.Fatalf("%s allowed zero or two writes to one revision", stage)
		}
	}
}

// A tab can be viewing the default policy before anyone has ever saved a
// setting. Reusing that stack name must still invalidate its old revision.
func TestDefinitionRemovalFencesFirstProtectionSave(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	key := StackUpdateProtectionKey("app")
	_, initial, err := s.AppSettingWithRevision(ctx, key)
	if err != nil || initial != "" {
		t.Fatalf("expected absent initial policy, %q: %v", initial, err)
	}
	if err = s.RemoveStackDefinitionState(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.CompareAndSetAppSetting(ctx, key, `{"auto_rollback":true}`, initial, time.Now()); err != nil || ok {
		t.Fatalf("deleted definition accepted prior editor: %t %v", ok, err)
	}
	value, current, err := s.AppSettingWithRevision(ctx, key)
	if err != nil || current == "" || value != `{"auto_rollback":false}` {
		t.Fatalf("new definition has no default policy revision: %q %q %v", value, current, err)
	}
}
