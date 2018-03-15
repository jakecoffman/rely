//+build debug

package rely

func debugf(s string, args... interface{}) {
	log.Debugf(s, args)
}
