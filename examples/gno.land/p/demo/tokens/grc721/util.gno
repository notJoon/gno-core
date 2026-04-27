package grc721

func isValidAddress(addr address) error {
	if !addr.IsValid() {
		return ErrInvalidAddress
	}
	return nil
}
