Tiger Tonic
===========

"The tiger said it didn't want to serve XML. Nobody says no to us…"

For when you're using [Tiger Tonic](http://github.com/rcrowley/go-tigertonic) and you
somehow need this. God save us all…

Import along with TigerTonic.

```
import (
    pdt "github.com/micrypt/punch-drunk-tiger/xml_marshaller"
    tt  "github.com/rcrowley/go-tigertonic"
)
```

Use as expected.

```
mux.Handle(
            "GET",
            "/api/find/{username}",
            tt.Timed(pdt.Marshaled(forwardGet), "GET-call", nil),
            )
```
