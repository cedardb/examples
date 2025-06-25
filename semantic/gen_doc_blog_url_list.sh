#!/bin/bash

cd $HOME/git/docs/content/
find . -name '*.md' | perl -ne 'chomp; s~^\./~~; s~_index\.md~~; s~\.md$~/~; print "https://cedardb.com/docs/$_\n"; '
cd $HOME/git/redesigned_website/src/public/content/
find blog -name 'index.md' | perl -ne 'chomp; s/index.md$//; print "https://cedardb.com/$_\n"; '
cd - > /dev/null

