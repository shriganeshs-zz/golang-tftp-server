package main

const (
	RRQ uint16 = 1
	WRQ uint16 = 2
	DAT uint16 = 3
	ACK uint16 = 4
	ERR uint16 = 5

	BLOCKSIZE       = 512
	TIMEOUT         = 3 // 3 sec timeout
	ZERO_BYTE       = 0
	MAX_BUFFER_SIZE = 1024

	// modes
	OCTET    = 1
	NETASCII = 2

	// Error codes
	UNKNOWN_ERROR     uint16 = 0 // Not defined, see error message (if any).
	NOT_FOUND         uint16 = 1 // File not found.
	ACCESS_VIOLATION  uint16 = 2 // Access violation.
	DISK_FULL         uint16 = 3 // Disk full or allocation exceeded.
	ILLEGAL_OPERATION uint16 = 4 // Illegal TFTP operation.
	UNKNOWN_ID        uint16 = 5 // Unknown transfer ID.
	ALREADY_EXISTS    uint16 = 6 // File already exists.
	NO_SUCH_USER      uint16 = 7 // No such user.
)
