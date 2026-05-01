package model

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
)

var gzipMagic = []byte{0x44, 0x44, 0x47, 0x5a, 0x31} // "DDGZ1"

func EncodeDdMessage(msg *DdMessage, compress bool) ([]byte, error) {
	msg.Normalize()
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if !compress {
		return raw, nil
	}
	var buf bytes.Buffer
	buf.Write(gzipMagic)
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeDdMessage(data []byte, out *DdMessage) error {
	payload := data
	if isGzipEnvelope(data) {
		zr, err := gzip.NewReader(bytes.NewReader(data[len(gzipMagic):]))
		if err != nil {
			return err
		}
		decoded, err := io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			return err
		}
		payload = decoded
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return err
	}
	out.Normalize()
	return nil
}

func isGzipEnvelope(data []byte) bool {
	if len(data) < len(gzipMagic) {
		return false
	}
	for i := range gzipMagic {
		if data[i] != gzipMagic[i] {
			return false
		}
	}
	return true
}
