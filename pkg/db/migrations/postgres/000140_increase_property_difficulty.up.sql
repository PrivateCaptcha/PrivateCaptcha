UPDATE backend.properties
SET level = CASE
WHEN level = '{{ .NewSmallLevel }}'::smallint + (-1) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + (-1) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 0) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 0) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 1) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 1) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 2) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 2) * '{{ .NextDelta }}'::smallint
END
WHERE level IN (
    '{{ .NewSmallLevel }}'::smallint - '{{ .NewDelta }}'::smallint,
    '{{ .NewSmallLevel }}'::smallint,
    '{{ .NewSmallLevel }}'::smallint + '{{ .NewDelta }}'::smallint,
    '{{ .NewSmallLevel }}'::smallint + 2 * '{{ .NewDelta }}'::smallint
);
