package common

import "io"

func IoConsumeAll(reader io.Reader) (int64, error) {
	buf := make([]byte, 1024)
	var totalBytes int64 = 0
	n := 1
	for n >= 0 {
		var err error
		n, err = reader.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return totalBytes, err
		}
	}
	return totalBytes, nil
}
