package engine

import "github.com/jolovicdev/hayduk/internal/protocol"

// profiles returns the saved msfrpcd endpoints. The list is available while
// disconnected (the connect dialog is the only caller), and it never carries
// a password.
func (e *Engine) profiles() []protocol.ProfilePayload {
	if e.cfg.Store == nil {
		return []protocol.ProfilePayload{}
	}
	profiles := e.cfg.Store.ListProfiles()
	if profiles == nil {
		return []protocol.ProfilePayload{}
	}
	return profiles
}
