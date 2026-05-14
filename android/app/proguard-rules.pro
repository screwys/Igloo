-keepattributes Signature
-keepattributes *Annotation*

# Rhino publishes a Java ScriptEngine service descriptor, but Android does not
# ship javax.script. Igloo does not use the Java scripting service loader.
-dontwarn javax.script.**

# Room
-keep class * extends androidx.room.RoomDatabase { *; }
