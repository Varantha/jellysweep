package engine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

// discordWebhook is a fake Discord webhook that records request bodies.
type discordWebhook struct {
	mu     sync.Mutex
	bodies []string
	status int
}

func newDiscordWebhook(t *testing.T) (*discordWebhook, string) {
	t.Helper()
	w := &discordWebhook{status: http.StatusNoContent}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.mu.Lock()
		defer w.mu.Unlock()
		w.bodies = append(w.bodies, string(body))
		rw.WriteHeader(w.status)
	}))
	t.Cleanup(srv.Close)
	return w, srv.URL
}

func (w *discordWebhook) setStatus(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

// take returns the bodies received since the last call.
func (w *discordWebhook) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	bodies := w.bodies
	w.bodies = nil
	return bodies
}

// dropFromQueue removes an item's queue row the way the dequeue steps do,
// so the next run marks it again.
func (h *harness) dropFromQueue(m *mediaRef) {
	h.t.Helper()
	row := h.assertMarked(m)
	row.DBDeleteReason = database.DBDeleteReasonStreamed
	require.NoError(h.t, h.db.DeleteMediaItem(h.t.Context(), row))
}

func TestDiscordAnnouncesItemOnceAcrossRemarks(t *testing.T) {
	webhook, url := newDiscordWebhook(t)
	h := newHarness(t, withDiscord(url))
	first := h.addMovie("First Movie")

	h.mustRunCleanup()
	sent := webhook.take()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "First Movie")

	// Nothing new to mark: no message.
	h.mustRunCleanup()
	require.Empty(t, webhook.take())

	// The item drops out of the queue and is marked again alongside a new one.
	h.dropFromQueue(first)
	h.addMovie("Second Movie")
	h.mustRunCleanup()
	h.assertMarked(first)
	sent = webhook.take()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "Second Movie")
	require.NotContains(t, sent[0], "First Movie", "already announced items must not be repeated")

	// Only re-marked items: no message at all.
	h.dropFromQueue(first)
	h.mustRunCleanup()
	h.assertMarked(first)
	require.Empty(t, webhook.take())
}

func TestDiscordReannouncesAfterCleanupDelay(t *testing.T) {
	webhook, url := newDiscordWebhook(t)
	h := newHarness(t, withDiscord(url))
	movie := h.addMovie("Old Movie")

	h.mustRunCleanup()
	require.Len(t, webhook.take(), 1)

	// The previous announcement is older than the 30 day default cleanup delay.
	h.dropFromQueue(movie)
	require.NoError(t, h.gdb.Model(&database.DiscordAnnouncement{}).
		Where("media_type = ? AND arr_id = ?", movie.mediaType, movie.arrID).
		Update("announced_at", time.Now().Add(-31*24*time.Hour)).Error)

	h.mustRunCleanup()
	sent := webhook.take()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "Old Movie")
}

func TestDiscordFailedAnnouncementIsNotRecorded(t *testing.T) {
	webhook, url := newDiscordWebhook(t)
	webhook.setStatus(http.StatusInternalServerError)
	h := newHarness(t, withDiscord(url))
	movie := h.addMovie("Old Movie")

	h.mustRunCleanup()
	require.Len(t, webhook.take(), 1)

	webhook.setStatus(http.StatusNoContent)
	h.dropFromQueue(movie)
	h.mustRunCleanup()
	sent := webhook.take()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "Old Movie")
}

func TestDiscordDryRunIsNotRecorded(t *testing.T) {
	webhook, url := newDiscordWebhook(t)
	h := newHarness(t, withDiscord(url), withDryRun())
	movie := h.addMovie("Old Movie")

	h.mustRunCleanup()
	require.Empty(t, webhook.take(), "dry run must not call the webhook")

	h.cfg.DryRun = false
	h.dropFromQueue(movie)
	h.mustRunCleanup()
	sent := webhook.take()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "Old Movie")
}
