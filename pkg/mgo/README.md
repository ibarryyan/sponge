## mgo

Mgo is based on the official library [mongo](https://github.com/mongodb/mongo-go-driver).

<br>

### Example of use

```go
    import "github.com/zhufuyi/sponge/pkg/mgo"

    var (
        dsn = "mongodb://root:123456@192.168.3.37:27017"
        dbName = "account"
    )
    db, err := mgo.Init(dsn, dbName)

    defer Close(db)
```
