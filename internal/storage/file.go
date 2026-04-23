package storage

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
)

type Record struct {
	UUID        string `json:"uuid"`
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

type FileStorage struct {
	mem     *MemStorage
	file    *os.File
	encoder *json.Encoder
	nextID  int
}

func NewFileStorage(path string) (*FileStorage, error) {
	mem := NewMemStorage()

	nextID, err := loadRecords(path, mem)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	return &FileStorage{
		mem:     mem,
		file:    file,
		encoder: json.NewEncoder(file),
		nextID:  nextID,
	}, nil
}

func loadRecords(path string, mem *MemStorage) (int, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	nextID := 0
	for {
		var rec Record
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		mem.Save(rec.ShortURL, rec.OriginalURL)
		if n, convErr := strconv.Atoi(rec.UUID); convErr == nil && n > nextID {
			nextID = n
		}
	}
	return nextID, nil
}

func (s *FileStorage) Save(id, url string) {
	s.mem.Save(id, url)
	s.nextID++
	rec := &Record{
		UUID:        strconv.Itoa(s.nextID),
		ShortURL:    id,
		OriginalURL: url,
	}
	if err := s.encoder.Encode(rec); err != nil {
		log.Printf("failed to persist record: %v", err)
	}
}

func (s *FileStorage) Get(id string) (string, bool) {
	return s.mem.Get(id)
}

func (s *FileStorage) Close() error {
	return s.file.Close()
}
