package models

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	pdb "pastellive/internal/db"
)

func newPlaylistID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "PL_" + hex.EncodeToString(b)
}

func CreatePlaylist(d *pdb.DB, userID int64, title, privacy, ip string) (playlistID string, err error) {
	playlistID = newPlaylistID()
	_, err = d.Exec("INSERT INTO user_playlists (playlist_id, user_id, title, privacy, ip_address) VALUES (?, ?, ?, ?, ?)", playlistID, userID, title, privacy, ip)
	return playlistID, err
}

type PlaylistItem struct {
	PlaylistID string
	Title      string
	Privacy    string
	CreatedAt  string
	VideoCount int64
}

func GetPlaylists(d *pdb.DB, userID int64) ([]PlaylistItem, error) {
	rows, err := d.Query(
		"SELECT p.playlist_id, p.title, p.privacy, p.created_at, "+
			"(SELECT COUNT(*) FROM user_playlist_videos v WHERE v.playlist_id = p.playlist_id) "+
			"FROM user_playlists p WHERE p.user_id = ? ORDER BY p.created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistItem
	for rows.Next() {
		var p PlaylistItem
		if err := rows.Scan(&p.PlaylistID, &p.Title, &p.Privacy, &p.CreatedAt, &p.VideoCount); err == nil {

			p.CreatedAt = strings.TrimSuffix(strings.Replace(p.CreatedAt, "T", " ", 1), "Z")
			out = append(out, p)
		}
	}
	if out == nil {
		out = []PlaylistItem{}
	}
	return out, rows.Err()
}
