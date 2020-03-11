package dns

import (
	"encoding/base32"
	"errors"
)

//
//  UTILS
//

// Dotify - dotifies a string
func Dotify(data []byte) (string, error) {

	b32s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data[:])
	bs := 60 // must be smaller than 62 or 63???
	l := len(b32s)
	if l > 241 { // double-check limit
		return "", errors.New("dotify - DNS maxlen is 253 for FQDN, so 241 before dotify")
	}
	i := l / bs
	var output []byte
	x := 0
	for x = 0; x < i; x++ {
		tmp := []byte(b32s)[x*bs : (x*bs)+bs]
		tmp = append(tmp, '.')
		output = append(output, tmp...)
	}
	tmp := []byte(b32s)[x*bs:]
	tmplen := byte(len(tmp))
	if tmplen > 0 {
		tmp = append(tmp, '.')
		output = append(output, tmp...)
	}
	return string(output), nil
}

// Undotify - un-dotifies a string
func Undotify(data string) ([]byte, error) {

	datalen := len(data)
	output := ""

	for i := 0; i < datalen; i++ {
		var b byte
		b = data[i]

		if b == '.' {
			continue
		}
		output += string(b)
	}
	outbytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(output)
	if err != nil {
		return nil, err
	}
	return outbytes, nil
}
