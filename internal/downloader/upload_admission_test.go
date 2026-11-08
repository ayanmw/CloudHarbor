package downloader

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type uploadNamespaceWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *uploadNamespaceWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestUploadLeaseSharesScanReservationUntilLastMember(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected HTTP") })
	first, releaseFirst, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, releaseSecond, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !d.validUploadLease(first) || !d.validUploadLease(second) {
		t.Fatal("group members not recognized")
	}
	if err = d.reserveCloudInventoryScan("busy"); err == nil {
		t.Fatal("scan admitted during upload group")
	}
	releaseFirst()
	releaseFirst()
	if d.validUploadLease(first) || !d.validUploadLease(second) {
		t.Fatal("release crossed member identity")
	}
	if err = d.reserveCloudInventoryScan("busy"); err == nil {
		t.Fatal("scan admitted before last member left")
	}
	releaseSecond()
	if err = d.reserveCloudInventoryScan("busy"); err != nil {
		t.Fatalf("reservation leaked: %v", err)
	}
	d.inventoryScanMutex.Lock()
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
}

func TestUploadLeaseRejectsExternalScanAndForeignOrExpiredContext(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	if err := d.reserveCloudInventoryScan("busy"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.acquireUploadLease(context.Background()); err == nil {
		t.Fatal("upload joined external reservation")
	}
	d.inventoryScanMutex.Lock()
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
	member, release, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	if other.validUploadLease(member) || d.validUploadLease(context.Background()) {
		t.Fatal("foreign context admitted")
	}
	release()
	if d.validUploadLease(member) {
		t.Fatal("released context remained valid")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = d.acquireUploadLease(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire=%v", err)
	}
}

func TestUploadNamespaceWaitIsExclusiveAndCancellable(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	member, release, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	leave, err := d.acquireUploadNamespace(member)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithCancel(member)
	waiting := &uploadNamespaceWaitContext{Context: wait, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		release, e := d.acquireUploadNamespace(waiting)
		if e == nil {
			release()
		}
		done <- e
	}()
	select {
	case <-waiting.entered:
	case <-time.After(2 * time.Second):
		leave()
		cancel()
		t.Fatal("namespace waiter did not enter select")
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled namespace wait=%v", err)
		}
	case <-time.After(2 * time.Second):
		leave()
		t.Fatal("namespace waiter ignored cancellation")
	}
	leave()
	leave()
	next, err := d.acquireUploadNamespace(member)
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestUploadLeaseRejectsMaintenanceAndShutdownWithoutChangingReservation(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	if err := d.BeginMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.acquireUploadLease(context.Background()); err == nil {
		t.Fatal("first upload admitted during external maintenance")
	}
	d.EndMaintenance()
	d.cancel()
	if _, _, err := d.acquireUploadLease(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown acquire=%v", err)
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.uploadLeaseMembers != 0 || d.inventoryScanStarting {
		t.Fatal("rejected acquisition changed reservation")
	}
}

func TestUploadNamespaceRetainsReservationEvenIfMemberLeaves(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	member, release, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	leave, err := d.acquireUploadNamespace(member)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err = d.reserveCloudInventoryScan("busy"); err == nil {
		t.Fatal("scan admitted during retained namespace phase")
	}
	leave()
	if _, err = d.acquireUploadNamespace(member); err == nil {
		t.Fatal("expired member admitted namespace phase")
	}
	if err = d.reserveCloudInventoryScan("busy"); err != nil {
		t.Fatalf("namespace leaked reservation: %v", err)
	}
	d.inventoryScanMutex.Lock()
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
}

func TestUploadNamespaceSerializesConcurrentMembers(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	var wg sync.WaitGroup
	var mu sync.Mutex
	inside, maximum := 0, 0
	for i := 0; i < 8; i++ {
		member, release, err := d.acquireUploadLease(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer release()
			leave, err := d.acquireUploadNamespace(member)
			if err != nil {
				t.Error(err)
				return
			}
			defer leave()
			mu.Lock()
			inside++
			if inside > maximum {
				maximum = inside
			}
			mu.Unlock()
			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if maximum != 1 {
		t.Fatalf("concurrent namespace phases=%d", maximum)
	}
}

func TestUploadNamespaceRejectsMemberReleasedWhileWaiting(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	first, releaseFirst, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	leave, err := d.acquireUploadNamespace(first)
	if err != nil {
		t.Fatal(err)
	}
	second, releaseSecond, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waiting := &uploadNamespaceWaitContext{Context: second, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		exit, e := d.acquireUploadNamespace(waiting)
		if e == nil {
			exit()
		}
		done <- e
	}()
	select {
	case <-waiting.entered:
	case <-time.After(2 * time.Second):
		leave()
		releaseFirst()
		releaseSecond()
		t.Fatal("waiter not started")
	}
	releaseSecond()
	leave()
	releaseFirst()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("expired waiting member entered namespace")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expired waiter remained blocked")
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.uploadLeaseMembers != 0 || d.inventoryScanStarting {
		t.Fatal("rejected waiter leaked reservation")
	}
}

func TestUploadOwnedMaintenanceAllowsOnlyLiveGroupPathReaders(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	member, release, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	leave, err := d.acquireUploadNamespace(member)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	if err = d.beginUploadMaintenance(member); err != nil {
		t.Fatal(err)
	}
	_, done, err := d.startPathSensitiveOperation(member)
	if err != nil {
		d.EndMaintenance()
		t.Fatalf("group path reader rejected: %v", err)
	}
	done()
	if _, done, err = d.startPathSensitiveOperation(context.Background()); err == nil {
		done()
		d.EndMaintenance()
		t.Fatal("external path reader bypassed upload maintenance")
	}
	d.EndMaintenance()
	if err = d.BeginMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, done, err = d.startPathSensitiveOperation(member); err == nil {
		done()
		d.EndMaintenance()
		t.Fatal("group bypassed external maintenance")
	}
	d.EndMaintenance()
}

func TestExpiredUploadContextCannotFallBackToOrdinaryMutation(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	member, release, err := d.acquireUploadLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err = d.MutateCloud(member, CloudMutationRequest{Operation: "mkdir", DestinationDirectory: "/", NewName: "expired"}); err == nil {
		t.Fatal("expired context admitted mutation")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.actions) != 0 {
		t.Fatalf("expired context wrote: %v", p.actions)
	}
}
