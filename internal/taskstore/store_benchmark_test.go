package taskstore

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkLoadEachRawRead(b *testing.B) {
	const recordCount = 10000
	queueDir := filepath.Join(b.TempDir(), "queue")
	store, err := Open(queueDir, "/preview")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	records := make([]Record, recordCount)
	for id := range records {
		records[id] = Record{ID: int64(id + 1), Data: []byte(`{"version":1,"status":"done","mode":"keep"}`)}
	}
	if err := store.SaveBatch(records); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.LoadEachRaw(func(int64, json.RawMessage) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveNewJobsBatchVsIndividual(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		records := make([]Record, count)
		for id := range records {
			records[id] = Record{ID: int64(id + 1), Data: []byte(fmt.Sprintf(`{"version":1,"id":%d,"status":"pending"}`, id+1))}
		}
		for _, batch := range []bool{false, true} {
			name := "individual_save"
			if batch {
				name = "batch_save"
			}
			b.Run(fmt.Sprintf("jobs_%d/%s", count, name), func(b *testing.B) {
				queueDir := filepath.Join(b.TempDir(), "queue")
				store, err := Open(queueDir, "/preview")
				if err != nil {
					b.Fatal(err)
				}
				defer store.Close()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if batch {
						err = store.SaveBatch(records)
					} else {
						for _, record := range records {
							if err = store.Save(record.ID, record.Data); err != nil {
								b.Fatal(err)
							}
						}
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
