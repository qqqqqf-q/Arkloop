package queue

const (
	retryBaseSeconds = 1
	retryMaxSeconds  = 30
	retryMaxExponent = 5
)

func DefaultRetryDelaySeconds(attempts int) int {
	if attempts <= 0 {
		return retryBaseSeconds
	}
	exponent := attempts - 1
	if exponent > retryMaxExponent {
		exponent = retryMaxExponent
	}
	seconds := retryBaseSeconds * (1 << exponent)
	if seconds > retryMaxSeconds {
		return retryMaxSeconds
	}
	return seconds
}
