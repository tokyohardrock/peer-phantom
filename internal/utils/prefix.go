package utils

import (
	"bufio"
	"encoding/binary"
	"fmt"
)

func ReadMessageWithLengthPrefix(reader *bufio.Reader) ([]byte, error) {
	const fn = "peer.readMessageWithPrefix"

	prefix := make([]byte, 4)

	_, err := reader.Read(prefix)
	if err != nil {
		return nil, fmt.Errorf("%s during reading message length from stream: %w", fn, err)
	}

	msgLen := binary.BigEndian.Uint32(prefix)
	rawMessage := make([]byte, msgLen)

	_, err = reader.Read(rawMessage)
	if err != nil {
		return nil, fmt.Errorf("%s during reading message from stream: %w", fn, err)
	}

	return rawMessage, nil
}

func AddLengthPrefixToMessage(message []byte) []byte {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, uint32(len(message)))

	messageWithPrefix := make([]byte, 4+len(message))
	messageWithPrefix = append(prefix, message...)

	return messageWithPrefix
}
