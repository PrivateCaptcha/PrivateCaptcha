UPDATE backend.properties
SET level = CASE
WHEN level = '{{ .NewSmallLevel }}'::smallint + (-1) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + (-1) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 0) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 0) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 1) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 1) * '{{ .NextDelta }}'::smallint
WHEN level = '{{ .NewSmallLevel }}'::smallint + ( 2) * '{{ .NewDelta }}'::smallint THEN '{{ .NextSmallLevel }}'::smallint + ( 2) * '{{ .NextDelta }}'::smallint
ELSE GREATEST(LEAST(level + '{{ sub .NextSmallLevel .NewSmallLevel }}'::smallint, 255), 0)
END
WHERE level IS NOT NULL;
