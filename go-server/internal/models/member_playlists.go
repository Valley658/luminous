package models

import (
	"database/sql"
	"encoding/json"
	"strings"

	pdb "pastellive/internal/db"
)

type MemberPlaylists struct {
	ChannelID       sql.NullString
	MusicPlaylists  []string
	ShortsPlaylists []string
	ReplayPlaylists []string
}

func parseJSONPlaylists(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil
	}
	trimmed := make([]string, 0, len(out))
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			trimmed = append(trimmed, s)
		}
	}
	return trimmed
}

func GetMemberPlaylists(d *pdb.DB, memberName string) (*MemberPlaylists, bool, error) {
	var channelID, music, shorts, replay sql.NullString
	err := d.QueryRow(
		"SELECT channel_id, music_playlists, shorts_playlists, replay_playlists FROM members WHERE member_name = ?",
		memberName,
	).Scan(&channelID, &music, &shorts, &replay)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &MemberPlaylists{
		ChannelID:       channelID,
		MusicPlaylists:  parseJSONPlaylists(music),
		ShortsPlaylists: parseJSONPlaylists(shorts),
		ReplayPlaylists: parseJSONPlaylists(replay),
	}, true, nil
}
