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
	if symbol == "" || len(symbol) > MaxSymbolLen {
		return false
	}
	for _, c := range symbol {
		if !isAlnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func validTokenID(tid TokenID) bool {
	value := tid.String()
	if value == "" || len(value) > MaxTokenIDLen {
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
