package coretest

import "github.com/bini69-oi/worker/core"

func (t *tx) SetPendingBuy(pb core.PendingBuy) error {
	t.st.carts[pb.UserID] = pb
	return nil
}

func (t *tx) PendingBuy(userID int64) (core.PendingBuy, error) {
	pb, ok := t.st.carts[userID]
	if !ok {
		return pb, core.ErrNotFound
	}
	return pb, nil
}

func (t *tx) DeletePendingBuy(userID int64) error {
	delete(t.st.carts, userID)
	return nil
}
