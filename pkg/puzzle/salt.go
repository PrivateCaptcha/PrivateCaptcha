package puzzle

import "hash/fnv"

type Salt struct {
	data        []byte
	fingerprint byte
}

func NewSalt(data []byte) *Salt {
	hash := fnv.New32a()
	hash.Write(data)
	fingerprint := hash.Sum32()

	return &Salt{
		data:        data,
		fingerprint: byte(fingerprint),
	}
}

func (s *Salt) Data() []byte {
	return s.data
}

func (s *Salt) Fingerprint() byte {
	return s.fingerprint
}
