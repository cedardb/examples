package web

// This file used to host a helper that re-built the (then only) Cedar Park
// circuit. Now that internal/sim/track.go has multiple presets, the web
// server picks the correct preset based on the live session's stored track
// name (see handleTrackJSON in server.go), so no helper is needed.
//
// Kept as a placeholder so any external references to internal/web/track.go
// from earlier branches don't break compilation. Safe to delete.
