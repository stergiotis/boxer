CREATE FUNCTION IF NOT EXISTS ANCHOR_UNFLATTEN_LEEWAY_ARRAY AS (flat_arr, lengths_arr) ->
    arrayMap(
            i -> arraySlice(
                    flat_arr,
                    toUInt64(arraySum(arraySlice(arrayPushFront(lengths_arr, 0), 1, i)) + 1),
                    toUInt64(lengths_arr[i])
                 ),
            range(1, length(lengths_arr) + 1)
    )