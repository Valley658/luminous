package models

import (
	pdb "pastellive/internal/db"
)

var KirinukiSeedChannelIDs = []string{
	"UCm4aofdqHmbuqAjaTuyE-Ow",
	"UCzFd7h5Cxofq0gOvV4RG99w",
	"UCOmWo_jU2xHLl4NzdnBIwtg",
	"UCwBLmucj95pImeUQekcWr0A",
	"UC2acHa6zvsnTo4P9OfNk4jQ",
	"UCuu0Lh2OuiHYuEOfdIEVLmQ",
	"UCFjFYOABBAAXmRASqlnNn1w",
	"UCSkxf8rEikMXb5Bgc92u_lQ",
	"UCu_mwEQTOoCplmwgsKS1AAw",
	"UC6Fyyza_69kOyObucJN3K3A",
	"UCRoUUrPmktQDckOzxnWoPtg",
}

func InitKirinukiChannelsTable(d *pdb.DB) error {
	if d.Backend == "mysql" {
		return nil
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS kirinuki_channels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_id VARCHAR(50) NOT NULL UNIQUE,
		channel_name VARCHAR(100),
		created_at DATETIME DEFAULT (datetime('now','localtime'))
	)`); err != nil {
		return err
	}
	for _, chID := range KirinukiSeedChannelIDs {
		_, _ = d.Exec("INSERT OR IGNORE INTO kirinuki_channels (channel_id, channel_name) VALUES (?, ?)", chID, "키리누키")
	}
	return nil
}

func GetKirinukiChannelIDs(d *pdb.DB) ([]string, error) {
	rows, err := d.Query("SELECT channel_id FROM kirinuki_channels ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
