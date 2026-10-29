package database

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
	"gorm.io/gorm/clause"
)

// DiscordAnnouncement records when a media item was last announced on Discord.
// Rows are keyed like the deletion queue and outlive the media row, so an item
// that drops out of the queue and is marked again is not announced twice.
type DiscordAnnouncement struct {
	MediaType   MediaType `gorm:"primaryKey"`
	ArrID       int32     `gorm:"primaryKey;autoIncrement:false"` // Sonarr or Radarr ID
	AnnouncedAt time.Time `gorm:"not null"`
}

func (c *Client) GetDiscordAnnouncements(ctx context.Context) ([]DiscordAnnouncement, error) {
	var announcements []DiscordAnnouncement
	if err := c.db.WithContext(ctx).Find(&announcements).Error; err != nil {
		log.Error("failed to get discord announcements", "error", err)
		return nil, err
	}
	return announcements, nil
}

// RecordDiscordAnnouncements upserts the announcement time of the given items.
func (c *Client) RecordDiscordAnnouncements(ctx context.Context, announcements []DiscordAnnouncement) error {
	if len(announcements) == 0 {
		return nil
	}
	err := c.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&announcements).Error
	if err != nil {
		log.Error("failed to record discord announcements", "error", err)
		return err
	}
	return nil
}
