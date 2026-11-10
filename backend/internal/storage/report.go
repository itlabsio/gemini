package storage

import (
	"encoding/json"
	"strings"
)

// DumpReport — что dump-Job сообщает инстансу Gemini через terminationMessage
// пода: ключ, sha256 и digest списка расширений ровно тех объектов, которые он
// залил. Подпись Source строится из этого отчёта, а не из содержимого бакета:
// в бакет пишут все Job-поды и target'ы с RW-ключами, статус пода — только kubelet.
type DumpReport struct {
	Key              string `json:"key"`
	SHA256           string `json:"sha256"`
	ExtensionsDigest string `json:"extensions_digest"`
}

// ParseDumpReport разбирает terminationMessage. Пусто, мусор или неполный
// отчёт — ok=false: подписывать тогда нечего.
func ParseDumpReport(msg string) (DumpReport, bool) {
	var r DumpReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &r); err != nil {
		return DumpReport{}, false
	}
	return r, r.Key != "" && r.SHA256 != "" && r.ExtensionsDigest != ""
}
