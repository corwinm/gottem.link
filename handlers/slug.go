package handlers

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
)

const generatedSlugLength = 7

var slugEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

type SlugGenerator func() (string, error)

func GenerateSlug() (string, error) {
	return generateSlug(rand.Reader)
}

func generateSlug(reader io.Reader) (string, error) {
	random := make([]byte, 5)
	if _, err := io.ReadFull(reader, random); err != nil {
		return "", fmt.Errorf("read random slug bytes: %w", err)
	}
	return slugEncoding.EncodeToString(random)[:generatedSlugLength], nil
}
