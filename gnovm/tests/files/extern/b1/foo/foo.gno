package foo

import bar "filetests/extern/b2/foo"

var Desc = "in b1/foo"

var Desc2 = Desc + bar.Desc
