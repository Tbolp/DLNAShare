package lib

import (
	"fmt"
	"sync/atomic"
)

const LOCKSTATUS = -1

type Status struct {
	status int32
}

func (this *Status) GetStatus() int {
	return int(atomic.LoadInt32(&this.status))
}

func (this *Status) LockStatus(status int) error {
	if atomic.CompareAndSwapInt32(&this.status, int32(status), -1) {
		return nil
	}
	return fmt.Errorf("Lock Failed")
}

func (this *Status) UnLockStatus(status int) error {
	if atomic.CompareAndSwapInt32(&this.status, -1, int32(status)) {
		return nil
	}
	return fmt.Errorf("UnLock Failed")
}

func (this *Status) LockMultiStatus(status ...int) (int, error) {
	for _, v := range status {
		if this.LockStatus(v) == nil {
			return v, nil
		}
	}
	return 0, fmt.Errorf("Lock Failed")
}

func (this *Status) Assign(cur int, status int) error {
	if atomic.CompareAndSwapInt32(&this.status, int32(cur), int32(status)) {
		return nil
	}
	return fmt.Errorf("Assign Failed")
}
