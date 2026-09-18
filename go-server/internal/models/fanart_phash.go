package models

import (
	"math/bits"
	"strconv"

	pdb "pastellive/internal/db"
)

func InsertFanartPhash(d *pdb.DB, fanartID int64, hash string) error {
	_, err := d.Exec("INSERT INTO fanart_phash (fanart_id, hash) VALUES (?, ?)", fanartID, hash)
	return err
}

func FindSimilarFanartPhash(d *pdb.DB, hash string, maxDistance int, excludeFanartID int64) (similarID int64, distance int, found bool, err error) {
	target, perr := strconv.ParseUint(hash, 16, 64)
	if perr != nil {
		return 0, 0, false, nil
	}

	rows, qerr := d.Query("SELECT fanart_id, hash FROM fanart_phash WHERE fanart_id != ?", excludeFanartID)
	if qerr != nil {
		return 0, 0, false, qerr
	}
	defer rows.Close()

	bestID := int64(0)
	bestDist := 65
	for rows.Next() {
		var id int64
		var h string
		if serr := rows.Scan(&id, &h); serr != nil {
			continue
		}
		v, perr2 := strconv.ParseUint(h, 16, 64)
		if perr2 != nil {
			continue
		}
		dist := bits.OnesCount64(target ^ v)
		if dist < bestDist {
			bestDist = dist
			bestID = id
		}
	}
	if rerr := rows.Err(); rerr != nil {
		return 0, 0, false, rerr
	}
	if bestID == 0 || bestDist > maxDistance {
		return 0, 0, false, nil
	}
	return bestID, bestDist, true, nil
}
