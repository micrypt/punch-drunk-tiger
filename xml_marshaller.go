package punchdrunktiger

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	tt "github.com/rcrowley/go-tigertonic"
)

// XMLMarshaler is an http.Handler that unmarshals XML input, handles the request
// via a function, and marshals XML output.  It refuses to answer requests
// without an Accept header that includes the application/xml content type.
type XMLMarshaler struct {
	v reflect.Value
}

// Marshaled returns an http.Handler that implements its ServeHTTP method by
// calling the given function, the signature of which must be
//
//     func(*url.URL, http.Header, *Request) (int, http.Header, *Response)
//
// where Request and Response may be any struct type of your choosing.
func Marshaled(i interface{}) *XMLMarshaler {
	t := reflect.TypeOf(i)
	if reflect.Func != t.Kind() {
		panic(tt.NewMarshalerError("kind was %v, not Func", t.Kind()))
	}
	if 3 != t.NumIn() && 4 != t.NumIn() {
		panic(tt.NewMarshalerError("input arity was %v, not 3 or 4", t.NumIn()))
	}
	if "*url.URL" != t.In(0).String() {
		panic(tt.NewMarshalerError(
			"type of first argument was %v, not *url.URL",
			t.In(0),
		))
	}
	if "http.Header" != t.In(1).String() {
		panic(tt.NewMarshalerError(
			"type of second argument was %v, not http.Header",
			t.In(1),
		))
	}
	if !t.In(2).Implements(reflect.TypeOf((*interface{})(nil)).Elem()) {
		panic(tt.NewMarshalerError(
			"type of third argument was %v, not some kind of interface{}",
			t.Out(2),
		))
	}
	if 4 != t.NumOut() {
		panic(tt.NewMarshalerError("output arity was %v, not 4", t.NumOut()))
	}
	if reflect.Int != t.Out(0).Kind() {
		panic(tt.NewMarshalerError(
			"type of first return value was %v, not int",
			t.Out(0),
		))
	}
	if "http.Header" != t.Out(1).String() {
		panic(tt.NewMarshalerError(
			"type of second return value was %v, not http.Header",
			t.Out(1),
		))
	}
	if !t.Out(2).Implements(reflect.TypeOf((*interface{})(nil)).Elem()) {
		panic(tt.NewMarshalerError(
			"type of third return value was %v, not Response",
			t.Out(2),
		))
	}
	if "error" != t.Out(3).String() {
		panic(tt.NewMarshalerError(
			"type of fourth return value was %v, not error",
			t.Out(3),
		))
	}
	return &XMLMarshaler{reflect.ValueOf(i)}
}

// ServeHTTP unmarshals XML input, handles the request via the function, and
// marshals XML output.
func (m *XMLMarshaler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wHeader := w.Header()
	if !acceptXML(r) {
		wHeader.Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotAcceptable)
		fmt.Fprintf(
			w,
			"\"%s\" does not contain \"application/xml\"",
			r.Header.Get("Accept"),
		)
		return
	}
	wHeader.Set("Content-Type", "application/xml")
	var rq reflect.Value
	in2 := m.v.Type().In(2)
	if reflect.Interface == in2.Kind() && 0 == in2.NumMethod() {
		rq = nilRequest
	} else if reflect.Slice == in2.Kind() || reflect.Map == in2.Kind() {
		// non-pointer maps/slices require special treatment because
		// xml.Unmarshal won't work on a non-pointer destination. We
		// add a level indirection here, then deref it before .Call()
		rq = reflect.New(in2)
	} else {
		rq = reflect.New(in2.Elem())
	}
	if "PATCH" == r.Method || "POST" == r.Method || "PUT" == r.Method {
		if rq == nilRequest {
			w.WriteHeader(http.StatusInternalServerError)
			writeXMLError(w, tt.NewMarshalerError(
				"empty interface is not suitable for %s request bodies",
				r.Method,
			))
			return
		}
		if !strings.HasPrefix(
			r.Header.Get("Content-Type"),
			"application/xml",
		) {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			writeXMLError(w, tt.NewMarshalerError(
				"Content-Type header is %s, not application/xml",
				r.Header.Get("Content-Type"),
			))
			return
		}
		decoder := reflect.ValueOf(xml.NewDecoder(r.Body))
		out := decoder.MethodByName("Decode").Call([]reflect.Value{rq})
		if !out[0].IsNil() {
			w.WriteHeader(http.StatusBadRequest)
			writeXMLError(w, out[0].Interface().(error))
			return
		}
		r.Body.Close()
	} else if nilRequest != rq {
		log.Printf(
			"%s request body isn't an empty interface; this is weird and is being ignored\n",
			r.Method,
		)
	}
	var out []reflect.Value
	// if we're dealing with a non-pointer map or slice, so we need to deref
	if in2.Kind() == reflect.Slice || reflect.Map == in2.Kind() {
		rq = rq.Elem()
	}
	if 3 == m.v.Type().NumIn() {
		out = m.v.Call([]reflect.Value{
			reflect.ValueOf(r.URL),
			reflect.ValueOf(r.Header),
			rq,
		})
	} else {
		out = m.v.Call([]reflect.Value{
			reflect.ValueOf(r.URL),
			reflect.ValueOf(r.Header),
			rq,
			reflect.ValueOf(tt.Context(r)),
		})
	}
	status := int(out[0].Int())
	header := out[1].Interface().(http.Header)
	rs := out[2].Interface()
	if !out[3].IsNil() {
		err := out[3].Interface().(error)
		if httpEquivError, ok := err.(tt.HTTPEquivError); ok {
			w.WriteHeader(httpEquivError.Status())
		} else if http.StatusContinue > status {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(status)
		}
		writeXMLError(w, err)
		return
	}
	if nil != header {
		for key, values := range header {
			wHeader.Del(key)
			for _, value := range values {
				wHeader.Add(key, value)
			}
		}
	}
	w.WriteHeader(status)
	if nil != rs && http.StatusNoContent != status {
		if err := xml.NewEncoder(w).Encode(rs); nil != err {
			log.Println(err)
		}
	}
}

var nilRequest = reflect.ValueOf((*interface{})(nil))

func acceptXML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if "" == accept {
		return true
	}
	return strings.Contains(accept, "*/*") || strings.Contains(accept, "application/xml")
}

func writeXMLError(w io.Writer, err error) {
	var e string
	if namedError, ok := err.(tt.NamedError); ok {
		e = namedError.Name()
	} else if httpEquivError, ok := err.(tt.HTTPEquivError); ok && tt.SnakeCaseHTTPEquivErrors {
		e = strings.Replace(strings.ToLower(http.StatusText(httpEquivError.Status())), " ", "_", -1)
	} else {
		t := reflect.TypeOf(err)
		if reflect.Ptr == t.Kind() {
			t = t.Elem()
		}
		e = t.String()
		if r, _ := utf8.DecodeRuneInString(t.Name()); unicode.IsLower(r) {
			e = "error"
		}
	}
	if err := xml.NewEncoder(w).Encode(map[string]string{
		"description": err.Error(),
		"error":       e,
	}); nil != err {
		log.Println(err)
	}
}
