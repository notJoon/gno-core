package grc721

const (
	MaxNameLen    = 64
	MaxSymbolLen  = 11
	MaxTokenIDLen = 128
)

func validAddress(addr address) bool {
	return addr.IsValid()
}

func validName(name string) bool {
	if name == "" || len(name) > MaxNameLen {
		return false
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func validSymbol(symbol string) bool {
	return validIdentifier(symbol, MaxSymbolLen)
}

func validTokenID(tid TokenID) bool {
	return validIdentifier(tid.String(), MaxTokenIDLen)
}

func validIdentifier(value string, maxLen int) bool {
	if value == "" || len(value) > maxLen {
		return false
	}
	for _, c := range value {
		if !isAlnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isAlnum(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
