#!/bin/bash

kill $( ps | egrep '(server|simulator)' | awk '{print $1}' )

