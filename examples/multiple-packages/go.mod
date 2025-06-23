module github.com/yuku/testdbpool/examples/multiple-packages

go 1.21

require (
	github.com/lib/pq v1.10.9
	github.com/yuku/testdbpool v0.0.0
)

replace github.com/yuku/testdbpool => ../..
