SET param_a = 13;
SET param_b = 'str';
SET param_c = '2022-08-04 18:30:53';
SELECT
    {a: UInt32},
    {b: String},
    {c: DateTime},
    {d: Map(String, Array(UInt8))}