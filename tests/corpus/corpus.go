// Package corpus defines fixed native compilation acceptance workloads.
package corpus

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type Case struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Expected string `json:"expected"`
}

func Cases() []Case {
	result := []Case{{"startup", "return 7\n", "7"}, {"list_sum", "return Math.sum(List.new(1,2,3,4))\n", "10"}}
	for _, size := range []int{1000, 10000} {
		items := make([]string, size)
		sum, filtered := int64(0), int64(0)
		for i := range items {
			n := int64((i*37 + 11) % 101)
			items[i] = strconv.FormatInt(n, 10)
			sum += n
			if n > 50 {
				filtered += n
			}
		}
		input := "items = List.new(" + strings.Join(items, ",") + ")\n"
		result = append(result,
			Case{fmt.Sprintf("map_%d", size), "flow F(x: Int) -> Int { return Math.mul(x, 3) }\n" + input + "return Math.sum(List.map(items, F))\n", strconv.FormatInt(sum*3, 10)},
			Case{fmt.Sprintf("filter_%d", size), "flow F(x: Int) -> Bool { return Math.gt(x, 50) }\n" + input + "return Math.sum(List.filter(items, F))\n", strconv.FormatInt(filtered, 10)},
			Case{fmt.Sprintf("fold_%d", size), "flow F(state: Int, x: Int) -> Int { return Math.add(state, x) }\n" + input + "return List.fold(items, 0, F)\n", strconv.FormatInt(sum, 10)},
			Case{fmt.Sprintf("records_%d", size), "flow F(x: Int) -> Int { r = Record.new(number: x, text: \"é界\"); return r.number }\n" + input + "return Math.sum(List.map(items, F))\n", strconv.FormatInt(sum, 10)},
		)
		large, _ := new(big.Int).SetString("100000000000000000000000000000000000000000000000000", 10)
		expected := new(big.Int).Add(large, big.NewInt(sum)).String()
		result = append(result, Case{fmt.Sprintf("bigint_%d", size), "flow F(state: Int, x: Int) -> Int { return Math.add(state, x) }\n" + input + "return List.fold(items, " + large.String() + ", F)\n", expected})
	}
	result = append(result, Case{"unicode", "return String.length(String.upper(" + strconv.Quote(strings.Repeat("aé界", 3000)) + "))\n", "9000"})
	return result
}
