package taskstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadEachRawReadConcurrency(b *testing.B) {
	const recordCount = 10000
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("tasks_%d/workers_%d", recordCount, workers), func(b *testing.B) {
			queueDir := filepath.Join(b.TempDir(), "queue")
			store, err := Open(queueDir, "/preview")
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			jobsDir := filepath.Join(queueDir, "jobs")
			for id := 1; id <= recordCount; id++ {
				name := filepath.Join(jobsDir, fmt.Sprintf("%d.json", id))
				if err := os.WriteFile(name, []byte(`{"version":1,"status":"done","mode":"keep"}`), 0600); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := store.loadEachWithWorkers(func(int64, json.RawMessage) error { return nil }, false, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
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
