.PHONY: build android android-aar android-apk clean

# Desktop binary
VERSION ?= dev
build:
	go build -ldflags="-X main.Version=$(VERSION)" -o acopy ./cmd/acopy

# Android: build AAR from Go, then build APK
android: android-aar android-apk

android-aar:
	gomobile bind -target=android -androidapi 26 -o android/app/libs/golib.aar ./golib

android-apk:
	cd android && ./gradlew assembleDebug

android-release: android-aar
	cd android && ./gradlew assembleRelease

clean:
	rm -f acopy
	rm -f android/app/libs/golib.aar
	cd android && ./gradlew clean 2>/dev/null || true
