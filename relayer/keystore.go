package relayer

import "strings"

const keyString = `{"address":"b415b39d33a828d1920b12aa4b49d4561bd77bbe","crypto":{"cipher":"aes-128-ctr","ciphertext":"5eb7068fef273ad765b841c034be3e468e14fc48313625368544fbeb05b40bf7","cipherparams":{"iv":"49bf65045c0528c48d6c04c5fbeeb004"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6bbda187ef179592e1978002a79a653eb641a814bb19fe77b4f22fb9b0f9d07f"},"mac":"5398894ce4870916914228bcc7ab56eb764b28aa0eee32d55f63487d35ec73e7"},"id":"2c1800b0-5abe-47fe-ab80-6a6fa2bd1807","version":3}`
const passParser = "123654789"

// GetKeyStoreReader return reader for keystore
func GetKeyStoreReader() *strings.Reader {
	return strings.NewReader(keyString)
}

// GetKeyStore return passparser and keystore reader
func GetKeyStore() (string, *strings.Reader) {
	return passParser, GetKeyStoreReader()
}
