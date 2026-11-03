package proto

// HeaderCCRX carries the sender's receive rate in bytes per second. The client
// sends it in the auth request; the server returns its own in the 233 response.
const HeaderCCRX = "Tianma-CC-RX"
