// PKGPATH: gno.land/r/test
package test

import "strconv"

// Store a reference to an existing stdlib object (not a copy).
var ref interface{}

func main(cur realm) {
	// This should store a reference to strconv.Itoa itself,
	// which is a FuncValue owned by the strconv package.
	// Does this incref the stdlib object?
	ref = strconv.Itoa
	println(ref.(func(int) string)(42))
}

// Output:
// 42

// Realm:
// finalizerealm["gno.land/r/test"]
// u[08ada09dee16d791fd406d629fe29bb0ed084a30:3](199)=
//     @@ -2,9 +2,39 @@
//          "ObjectInfo": {
//              "ID": "08ada09dee16d791fd406d629fe29bb0ed084a30:3",
//              "LastObjectSize": "190",
//     -        "ModTime": "0",
//     +        "ModTime": "5",
//              "OwnerID": "08ada09dee16d791fd406d629fe29bb0ed084a30:2",
//              "RefCount": "1"
//          },
//     -    "Value": {}
//     +    "Value": {
//     +        "T": {
//     +            "@type": "/gno.FuncType",
//     +            "Params": [
//     +                {
//     +                    "Embedded": false,
//     +                    "Name": "i",
//     +                    "Tag": "",
//     +                    "Type": {
//     +                        "@type": "/gno.PrimitiveType",
//     +                        "value": "32"
//     +                    }
//     +                }
//     +            ],
//     +            "Results": [
//     +                {
//     +                    "Embedded": false,
//     +                    "Name": ".res.0",
//     +                    "Tag": "",
//     +                    "Type": {
//     +                        "@type": "/gno.PrimitiveType",
//     +                        "value": "16"
//     +                    }
//     +                }
//     +            ]
//     +        },
//     +        "V": {
//     +            "@type": "/gno.RefValue",
//     +            "ObjectID": "c3ac87e8cb3cc9276228f74d38694a208cacb99b:869"
//     +        }
//     +    }
//      }
// u[c3ac87e8cb3cc9276228f74d38694a208cacb99b:869](7)=
//     @@ -8,10 +8,11 @@
//          "NativePkg": "",
//          "ObjectInfo": {
//              "ID": "c3ac87e8cb3cc9276228f74d38694a208cacb99b:869",
//     +        "IsEscaped": true,
//              "LastObjectSize": "447",
//     -        "ModTime": "0",
//     +        "ModTime": "5",
//              "OwnerID": "c3ac87e8cb3cc9276228f74d38694a208cacb99b:2",
//     -        "RefCount": "1"
//     +        "RefCount": "2"
//          },
//          "Parent": {
//              "@type": "/gno.RefValue",
