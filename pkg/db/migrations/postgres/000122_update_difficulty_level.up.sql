UPDATE backend.properties
SET level = CASE
WHEN level = {{ .OldSmallLevel }} + (-1) * {{ .OldDelta }} THEN {{ .NewSmallLevel }} + (-1) * {{ .NewDelta }}
WHEN level = {{ .OldSmallLevel }} + ( 0) * {{ .OldDelta }} THEN {{ .NewSmallLevel }} + ( 0) * {{ .NewDelta }}
WHEN level = {{ .OldSmallLevel }} + ( 1) * {{ .OldDelta }} THEN {{ .NewSmallLevel }} + ( 1) * {{ .NewDelta }}
WHEN level = {{ .OldSmallLevel }} + ( 2) * {{ .OldDelta }} THEN {{ .NewSmallLevel }} + ( 2) * {{ .NewDelta }}
WHEN level = {{ .OldSmallLevel }} + ( 3) * {{ .OldDelta }} THEN {{ .NewSmallLevel }} + ( 3) * {{ .NewDelta }}
ELSE GREATEST(LEAST(level + {{ sub .NewSmallLevel .OldSmallLevel }}, 255), 0)
END;
