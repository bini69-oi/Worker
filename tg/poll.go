package tg

import (
	"context"
	"time"
)

const (
	pollTimeout = 30 // секунд длинного опроса
	pollPause   = 3 * time.Second
)

// Poll забирает обновления через getUpdates и кладёт их в out, пока не закончится ctx.
// После ошибки делает паузу. Возвращает ctx.Err().
func Poll(ctx context.Context, c *Client, out chan<- Update) error {
	var offset int64
	for ctx.Err() == nil {
		ups, err := c.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if serr := c.sleep(ctx, pollPause); serr != nil {
				return serr
			}
			continue
		}
		for _, u := range ups {
			select {
			case out <- u:
			case <-ctx.Done():
				return ctx.Err()
			}
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
	}
	return ctx.Err()
}
