package utils

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	messageLengthLimit = 10 * 1024 // 10 Kb
	prefixLen          = 2
)

func ReadMessageWithLengthPrefix(reader io.Reader) ([]byte, error) {
	const fn = "peer.readMessageWithPrefix"

	prefix := make([]byte, prefixLen)

	_, err := reader.Read(prefix)
	if err != nil {
		return nil, fmt.Errorf("%s during reading message length from stream: %w", fn, err)
	}

	msgLen := binary.BigEndian.Uint16(prefix)
	rawMessage := make([]byte, msgLen)

	_, err = reader.Read(rawMessage)
	if err != nil {
		return nil, fmt.Errorf("%s during reading message from stream: %w", fn, err)
	}

	return rawMessage, nil
}

func AddLengthPrefixToMessage(message []byte) ([]byte, error) {
	const fn = "utils.AddLengthPrefixToMessage"

	if len(message) > messageLengthLimit {
		return nil, fmt.Errorf("%s: message is to big (max %d bytes)", fn, messageLengthLimit)
	}

	prefix := make([]byte, prefixLen)
	binary.BigEndian.PutUint16(prefix, uint16(len(message)))

	messageWithPrefix := make([]byte, prefixLen+len(message))
	messageWithPrefix = append(prefix, message...)

	return messageWithPrefix, nil
}
