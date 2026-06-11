if (!exists("data")) data = "/tmp/difficulty-sim.tsv"
if (!exists("term")) term = "qt persist"
if (!exists("outfile")) outfile = ""
if (!exists("plot_title")) plot_title = "Captcha difficulty simulation"
if (!exists("debug")) debug = 0

set terminal term
if (strlen(outfile) > 0) set output outfile

set datafile separator whitespace
set grid
set key outside right top box title "Legend"
set tics out
set border

if (debug) {
  set multiplot layout 3,1 title plot_title
} else {
  set multiplot layout 2,1 title plot_title
}

# Top panel: traffic signals and bucket-derived pressure.
set title "Traffic inputs and bucket state"
set xlabel ""
set ylabel "requests / bucket levels"
set yrange [0:*]
set y2range [0:*]
set ytics nomirror
set y2tics
set y2label "λ and P8"
set format x ""
plot \
  data using 1:2 with impulses title "property background req/step", \
  data using 1:3 with impulses title "selected user req/step", \
  data using 1:5 with lines linewidth 2 title "property level p", \
  data using 1:6 with lines linewidth 2 title "user level u", \
  data using 1:7 axes x1y2 with lines dashtype 3 title "property leakRate λ", \
  data using 1:8 axes x1y2 with lines dashtype 2 linewidth 2 title "P8=sqrt((minRPS*bucket)^2+(K*λ)^2)"

if (debug) {
  # Debug panel: normalized quantities and float deltas before byte rounding.
  set title "Debug: normalized pressure and float difficulty components"
  set xlabel ""
  set ylabel "ratios / pressure"
  set y2label "difficulty delta"
  set yrange [0:*]
  set y2range [0:*]
  set format x ""
  plot \
    data using 1:15 with lines linewidth 2 title "property_ratio p/P8", \
    data using 1:16 with lines linewidth 2 title "user_ratio u/U8", \
    data using 1:14 with lines dashtype 3 title "property_excess_buckets p/λ", \
    data using 1:29 with impulses title "instant rate ratio", \
    data using 1:23 axes x1y2 with lines linewidth 2 title "user_delta_float", \
    data using 1:24 axes x1y2 with lines linewidth 2 title "property_delta_float", \
    data using 1:26 axes x1y2 with lines linewidth 3 title "total_delta_float"
}

# Bottom panel: byte difficulty. V1/V2 are always plotted; selected difficulty is controlled by --formula.
set title "Difficulty output"
set xlabel "time (s)"
set ylabel "difficulty byte"
unset y2label
unset y2tics
set ytics mirror
set yrange [0:255]
set format x
if (debug) {
  plot \
    data using 1:9 with lines dashtype 3 title "V1 legacy: f(u+p)", \
    data using 1:10 with lines linewidth 2 title "V2 candidate: rounded", \
    data using 1:11 with lines linewidth 3 title "selected --formula", \
    data using 1:27 with lines dashtype 2 title "V2 float before rounding"
} else {
  plot \
    data using 1:9 with lines dashtype 3 title "V1 legacy: f(u+p)", \
    data using 1:10 with lines linewidth 2 title "V2 candidate: f(u,p,λ,K)", \
    data using 1:11 with lines linewidth 3 title "selected --formula"
}

unset multiplot

print "How to read this chart:"
print "  Top panel: impulses are generated requests per step; solid lines are leaky-bucket levels."
print "  property level p is compared against P8=sqrt((minExpectedRPS*bucketSize)^2 + (K*leakRate)^2)."
print "  When p≈P8, the property term contributes one normalized unit: +8*g*propertyWeight difficulty."
print "  Bottom panel: V1 is the old combined-level formula; V2 is the new split user/property formula."
if (debug) {
  print "  Debug panel: p/P8 is the normalized property signal; property_delta_float shows its actual contribution before rounding."
  print "  If p/P8 stays small, the formula is being fed weak property pressure."
  print "  If P8 or leakRate rises during the spike, the denominator is chasing the numerator."
  print "  property_ref_from_min_rps shows the smooth low-traffic component: minExpectedRPS * propertyBucketSize."
  print "  instant rate ratio compares current per-step property requests to leakRate scaled down to the simulation step."
}

if (strstrt(term, "qt") == 1) pause mouse close
