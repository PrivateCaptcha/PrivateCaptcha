UPDATE backend.properties
SET level = CASE
WHEN level = '{{ .NextSmallLevel }}'::smallint + (-1) * '{{ .NextDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + (-1) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .NextSmallLevel }}'::smallint + ( 0) * '{{ .NextDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 0) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .NextSmallLevel }}'::smallint + ( 1) * '{{ .NextDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 1) * '{{ .NewDelta }}'::smallint
WHEN level = '{{ .NextSmallLevel }}'::smallint + ( 2) * '{{ .NextDelta }}'::smallint THEN '{{ .NewSmallLevel }}'::smallint + ( 2) * '{{ .NewDelta }}'::smallint
END
WHERE level IN (
    '{{ .NextSmallLevel }}'::smallint - '{{ .NextDelta }}'::smallint,
    '{{ .NextSmallLevel }}'::smallint,
    '{{ .NextSmallLevel }}'::smallint + '{{ .NextDelta }}'::smallint,
    '{{ .NextSmallLevel }}'::smallint + 2 * '{{ .NextDelta }}'::smallint
);
