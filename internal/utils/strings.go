package utils

import "unsafe"

func ConcatenateStrings(seq ...string) string {
	var length, byteIdx int
	for i := range seq {
		length += len(seq[i])
	}

	if length == 0 {
		return ""
	}

	b := make([]byte, length)
	for i := range seq {
		byteIdx += copy(b[byteIdx:], seq[i])
	}

	return unsafe.String(&b[0], length)
}

func GetShortPeerID(peerID string) string {
	if len(peerID) > 6 {
		return peerID[len(peerID)-6:]
	}

	return peerID
}
