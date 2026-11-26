package bot

import "context"

// Channels проверяет подписку на канал через getChatMember. Это реализация core.Channels.
type Channels struct{ API API }

// IsMember говорит, подписан ли человек: участник, админ или владелец канала.
func (c Channels) IsMember(ctx context.Context, channel string, tgID int64) (bool, error) {
	status, err := c.API.GetChatMember(ctx, channel, tgID)
	if err != nil {
		return false, err
	}
	switch status {
	case "member", "administrator", "creator", "restricted":
		return true, nil
	}
	return false, nil
}
